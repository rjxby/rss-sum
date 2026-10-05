import argparse
from contextlib import contextmanager
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import signal
import queue
import threading
import ssl
from http.server import BaseHTTPRequestHandler, HTTPServer
import socket
import subprocess
import sys
import tarfile
import tempfile
import time
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[1] / 'setup/local_stack.py'
spec = importlib.util.spec_from_file_location('local_stack', SCRIPT)
stack = importlib.util.module_from_spec(spec)
spec.loader.exec_module(stack)


class StackTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='rss-setup-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.prefix = self.root / 'installed rss-sum'

    def marker(self, complete=True):
        self.prefix.mkdir(mode=0o700)
        stack.write_json(self.prefix / stack.MARKER,
                         {'schema': 1, 'prefix': str(self.prefix), 'complete': complete})

    def test_default_provider_uses_gen_proxy_with_separate_keys(self):
        self.marker()
        stack.write_json(self.prefix / 'keys.json', {'public': 'public-key', 'runtime': 'runtime-key'})
        with patch.dict(os.environ, {'GEN_PROXY_BASE_URL': 'http://elsewhere:9000', 'DATABASE_PATH': '/elsewhere'}):
            env = stack.app_environment(self.prefix, stack.DEFAULTS)
        self.assertEqual('http://127.0.0.1:7001', env['GEN_PROXY_BASE_URL'])
        self.assertEqual('public-key', env['GEN_PROXY_API_KEY'])
        self.assertEqual('data/rss-sum.sqlite', env['DATABASE_PATH'])

    def test_defaults_and_cli_overrides_do_not_prompt(self):
        args = stack.parser().parse_args(['install', '--defaults', '--http-port', '8090'])
        with patch('builtins.input', side_effect=AssertionError('Unexpected prompt')):
            config = stack.configuration(args)
        self.assertEqual(8090, config['http_port'])
        self.assertEqual(stack.DEFAULTS['model_sha256'], config['model_sha256'])

    def test_wizard_applies_answers(self):
        answers = ['8091'] + [''] * (len(stack.DEFAULTS) - 1)
        with patch('sys.stdin.isatty', return_value=True), patch('builtins.input', side_effect=answers):
            config = stack.configuration(stack.parser().parse_args(['install']))
        self.assertEqual(8091, config['http_port'])

    def test_noninteractive_setup_requires_defaults(self):
        with patch('sys.stdin.isatty', return_value=False), self.assertRaises(stack.SetupError):
            stack.configuration(stack.parser().parse_args(['install']))

    def test_defaults_do_not_authorize_downloads(self):
        args = stack.parser().parse_args(['install', '--defaults', '--prefix', str(self.prefix)])
        with patch('sys.stdin.isatty', return_value=False), patch.object(stack, 'check_ports'), \
                patch.object(stack, 'copy_application'), patch.object(stack, 'install_dependencies') as dependencies, \
                patch.object(stack, 'download') as download, patch.object(stack, 'create_certificates'), \
                patch.object(stack, 'local_stack'):
            with self.assertRaisesRegex(stack.SetupError, 'accept-downloads'):
                stack.install(args)
        dependencies.assert_not_called()
        download.assert_not_called()
        self.assertFalse(self.prefix.exists())

    def test_declining_downloads_leaves_no_installation(self):
        args = stack.parser().parse_args(['install', '--defaults', '--prefix', str(self.prefix)])
        for answer in ('', 'n', 'no', 'maybe'):
            with self.subTest(answer=answer), patch('sys.stdin.isatty', return_value=True), \
                    patch('builtins.input', return_value=answer), patch.object(stack, 'check_ports'), \
                    patch.object(stack, 'copy_application'), patch.object(stack, 'install_dependencies') as dependencies, \
                    patch.object(stack, 'download') as download, patch.object(stack, 'create_certificates'), \
                    patch.object(stack, 'local_stack'):
                stack.install(args)
                dependencies.assert_not_called()
                download.assert_not_called()
                self.assertFalse(self.prefix.exists())

    def test_rpc_duration_covers_one_day_boundary(self):
        self.assertEqual('00:05:00', stack.duration(300))
        self.assertEqual('23:59:59', stack.duration(86399))
        self.assertEqual('1.00:00:00', stack.duration(86400))
        stack.validate_config({**stack.DEFAULTS, 'timeout': 86400})
        with self.assertRaises(stack.SetupError):
            stack.validate_config({**stack.DEFAULTS, 'timeout': 86401})

    def test_invalid_config_rejected(self):
        for key, value in [('http_port', 65536), ('interval', 0), ('model_sha256', 'bad'),
                           ('model_id', 'bad\nvalue'), ('feeds', 'file:///etc/passwd'),
                           ('model_url', 'http://huggingface.co/a.gguf'), ('generation_port', 7001)]:
            with self.subTest(key=key), self.assertRaises(stack.SetupError):
                stack.validate_config({**stack.DEFAULTS, key: value})

    def test_existing_unowned_directory_is_not_removed(self):
        self.prefix.mkdir()
        sentinel = self.prefix / 'keep.txt'
        sentinel.write_text('keep')
        with self.assertRaises(stack.SetupError):
            stack.uninstall(argparse.Namespace(prefix=str(self.prefix), yes=True))
        self.assertEqual('keep', sentinel.read_text())

    def test_marker_cannot_be_reused_for_another_directory(self):
        self.marker()
        data = {'schema': 1, 'prefix': str(self.root), 'complete': True}
        stack.write_json(self.prefix / stack.MARKER, data)
        with self.assertRaises(stack.SetupError):
            stack.uninstall(argparse.Namespace(prefix=str(self.prefix), yes=True))
        self.assertTrue(self.prefix.exists())

    def test_prefix_symlink_rejected(self):
        self.marker()
        link = self.root / 'symlink'
        link.symlink_to(self.prefix, target_is_directory=True)
        with self.assertRaises(stack.SetupError):
            stack.installation_path(link)

    def test_incomplete_install_can_be_removed(self):
        self.marker(complete=False)
        for name in ('models', 'data'):
            (self.prefix / name).mkdir()
            (self.prefix / name / 'file').write_text('owned')
        sibling = self.root / 'keep'
        sibling.write_text('untouched')
        stack.uninstall(argparse.Namespace(prefix=str(self.prefix), yes=True))
        self.assertFalse(self.prefix.exists())
        self.assertEqual('untouched', sibling.read_text())
        stack.uninstall(argparse.Namespace(prefix=str(self.prefix), yes=True))

    def test_live_install_lock_blocks_cleanup(self):
        self.marker()
        with stack.install_lock(self.prefix), self.assertRaises(stack.SetupError):
            stack.uninstall(argparse.Namespace(prefix=str(self.prefix), yes=True))
        self.assertTrue(self.prefix.exists())

    def test_archive_escape_is_rejected(self):
        for name, link in [('../escape', None), ('/absolute', None), ('link', '../../escape')]:
            with self.subTest(name=name):
                archive_path = self.root / 'runtime.tar.gz'
                with tarfile.open(archive_path, 'w:gz') as archive:
                    member = tarfile.TarInfo(name)
                    if link:
                        member.type = tarfile.SYMTYPE
                        member.linkname = link
                    else:
                        member.size = 4
                    archive.addfile(member, None if link else io.BytesIO(b'test'))
                destination = self.root / 'runtime'
                destination.mkdir(exist_ok=True)
                with self.assertRaises(stack.SetupError):
                    stack.extract_runtime(archive_path, destination)
        self.assertFalse((self.root / 'escape').exists())

    def test_internal_runtime_symlinks_supported(self):
        archive_path = self.root / 'runtime.tar.gz'
        with tarfile.open(archive_path, 'w:gz') as archive:
            member = tarfile.TarInfo('lib.so.1')
            member.size = 4
            archive.addfile(member, io.BytesIO(b'test'))
            member = tarfile.TarInfo('lib.so')
            member.type = tarfile.SYMTYPE
            member.linkname = 'lib.so.1'
            archive.addfile(member)
        destination = self.root / 'runtime'
        destination.mkdir()
        stack.extract_runtime(archive_path, destination)
        self.assertEqual(b'test', (destination / 'lib.so').read_bytes())

    def test_checksum_mismatch_does_not_proceed(self):
        with patch.object(stack.urllib.request, 'urlopen', return_value=io.BytesIO(b'wrong')):
            with self.assertRaisesRegex(stack.SetupError, 'SHA-256'):
                stack.download('https://example.com/model', self.root / 'model', '0' * 64)

    def test_verified_download(self):
        data = b'model'
        with patch.object(stack.urllib.request, 'urlopen', return_value=io.BytesIO(data)):
            stack.download('https://example.com/model', self.root / 'model', hashlib.sha256(data).hexdigest())
        self.assertEqual(data, (self.root / 'model').read_bytes())

    def test_environment_isolates_native_stack(self):
        with patch.dict(os.environ, {'Kestrel__Endpoints__Evil__Url': 'http://0.0.0.0:80',
                                     'Inference__WorkerCount': '100', 'DOTNET_ROOT': '/elsewhere'}):
            env = stack.stack_environment(self.prefix)
        self.assertNotIn('Kestrel__Endpoints__Evil__Url', env)
        self.assertNotIn('Inference__WorkerCount', env)
        self.assertNotIn('DOTNET_ROOT', env)
        self.assertEqual(str(self.prefix / 'runtime-home'), env['HOME'])

    def test_install_failure_keeps_cleanup_marker(self):
        args = stack.parser().parse_args(['install', '--defaults', '--accept-downloads',
                                         '--build-backends', '--prefix', str(self.prefix)])
        with patch.object(stack, 'check_ports'), patch.object(stack, 'copy_application'), \
                patch.object(stack, 'install_dependencies', side_effect=stack.SetupError('download failed')):
            with self.assertRaisesRegex(stack.SetupError, 'download failed'):
                stack.install(args)
        self.assertFalse(stack.read_marker(self.prefix)['complete'])
        self.assertTrue((self.prefix / 'uninstall.sh').is_file())
        stack.uninstall(argparse.Namespace(prefix=str(self.prefix), yes=True))

    def test_accepting_downloads_proceeds_to_dependency_install(self):
        args = stack.parser().parse_args(['install', '--defaults', '--build-backends',
                                         '--prefix', str(self.prefix)])
        with patch('sys.stdin.isatty', return_value=True), patch('builtins.input', return_value='YES'), \
                patch.object(stack, 'check_ports'), patch.object(stack, 'copy_application'), \
                patch.object(stack, 'install_dependencies', side_effect=stack.SetupError('fixture stop')) as dependencies:
            with self.assertRaisesRegex(stack.SetupError, 'fixture stop'):
                stack.install(args)
        dependencies.assert_called_once_with(self.prefix, None, True)
        self.assertFalse(stack.read_marker(self.prefix)['complete'])

    def test_explicit_consent_does_not_prompt(self):
        args = stack.parser().parse_args(['install', '--defaults', '--accept-downloads',
                                         '--build-backends', '--prefix', str(self.prefix)])
        with patch('sys.stdin.isatty', return_value=False), \
                patch('builtins.input', side_effect=AssertionError('Unexpected prompt')), \
                patch.object(stack, 'check_ports'), patch.object(stack, 'copy_application'), \
                patch.object(stack, 'install_dependencies', side_effect=stack.SetupError('fixture stop')):
            with self.assertRaisesRegex(stack.SetupError, 'fixture stop'):
                stack.install(args)

    def test_end_of_input_cancels_before_downloads(self):
        args = stack.parser().parse_args(['install', '--defaults', '--prefix', str(self.prefix)])
        with patch('sys.stdin.isatty', return_value=True), patch('builtins.input', side_effect=EOFError), \
                patch.object(stack, 'check_ports'), patch.object(stack, 'download') as download:
            stack.install(args)
        download.assert_not_called()
        self.assertFalse(self.prefix.exists())

    def test_linux_without_backend_assets_fails_before_creating_files(self):
        args = stack.parser().parse_args(['install', '--defaults', '--accept-downloads',
                                         '--prefix', str(self.prefix)])
        with patch.object(stack.platform, 'system', return_value='Linux'), \
                patch.object(stack.platform, 'machine', return_value='x86_64'), \
                patch.object(stack, 'check_ports'), patch.object(stack, 'download') as download:
            with self.assertRaisesRegex(stack.SetupError, 'build-backends'):
                stack.install(args)
        download.assert_not_called()
        self.assertFalse(self.prefix.exists())

    def test_missing_explicit_bundle_does_not_download_or_build(self):
        with patch.object(stack, 'download_dependencies') as download, \
                patch.object(stack, 'build_dependencies') as build:
            with self.assertRaisesRegex(stack.SetupError, 'not found'):
                stack.install_dependencies(self.prefix, self.root / 'missing')
        download.assert_not_called()
        build.assert_not_called()

    def test_backend_downloads_verify_pinned_assets_and_extract_runtime(self):
        self.prefix.mkdir()
        for name in ('proxy', 'runtime'):
            (self.prefix / name).mkdir()
        payload = io.BytesIO()
        with tarfile.open(fileobj=payload, mode='w:gz') as archive:
            for name in ('LlamaRuntime.Presentation.Grpc', 'libllama_adapter.dylib'):
                member = tarfile.TarInfo(name)
                member.size = 7
                member.mode = 0o755
                archive.addfile(member, io.BytesIO(b'fixture'))
        releases = stack.BACKEND_RELEASES[('Darwin', 'arm64')]
        with patch.object(stack.platform, 'system', return_value='Darwin'), \
                patch.object(stack.platform, 'machine', return_value='arm64'), \
                patch.object(stack.urllib.request, 'urlopen',
                             side_effect=[io.BytesIO(b'proxy'), io.BytesIO(payload.getvalue())]) as urlopen:
            # Fixture bytes have their own hashes; production pins remain covered by the calls below.
            def fixture_download(url, destination, expected_hash):
                self.assertEqual(releases['gen-proxy' if destination.name == 'GenProxy.Api.Host'
                                          else 'llama-runtime'][1], expected_hash)
                data = b'proxy' if destination.name == 'GenProxy.Api.Host' else payload.getvalue()
                original_download(url, destination, hashlib.sha256(data).hexdigest())
            original_download = stack.download
            with patch.object(stack, 'download', side_effect=fixture_download), \
                    patch.object(stack, 'build_dependencies', side_effect=AssertionError('Unexpected build')):
                stack.install_dependencies(self.prefix, None)
        self.assertEqual([releases['gen-proxy'][0], releases['llama-runtime'][0]],
                         [call.args[0].full_url for call in urlopen.call_args_list])
        self.assertEqual(b'proxy', (self.prefix / 'proxy/GenProxy.Api.Host').read_bytes())
        self.assertEqual(b'fixture', (self.prefix / 'runtime/libllama_adapter.dylib').read_bytes())
        self.assertTrue(os.access(self.prefix / 'runtime/LlamaRuntime.Presentation.Grpc', os.X_OK))
        self.assertEqual(stack.GEN_PROXY_REVISION,
                         json.loads((self.prefix / 'versions.json').read_text())['gen_proxy'])

    def test_backend_checksum_failure_prevents_runtime_extraction(self):
        self.prefix.mkdir()
        (self.prefix / 'proxy').mkdir()
        with patch.object(stack.platform, 'system', return_value='Darwin'), \
                patch.object(stack.platform, 'machine', return_value='arm64'), \
                patch.object(stack.urllib.request, 'urlopen', return_value=io.BytesIO(b'wrong')) as urlopen, \
                patch.object(stack, 'extract_runtime') as extract:
            with self.assertRaisesRegex(stack.SetupError, 'SHA-256'):
                stack.download_dependencies(self.prefix)
        self.assertEqual(1, urlopen.call_count)
        extract.assert_not_called()

    def test_runtime_exit_before_readiness_fails(self):
        with stack.managed_process([sys.executable, '-c', 'pass']) as child:
            child.wait(timeout=5)
            with self.assertRaisesRegex(stack.SetupError, 'generation exited'):
                stack.wait_ready('http://127.0.0.1:1/health/ready', [('generation', child)], timeout=1)

    def test_child_is_stopped_after_owner_failure(self):
        with self.assertRaises(RuntimeError):
            with stack.managed_process([sys.executable, '-c', 'import time; time.sleep(120)']) as child:
                raise RuntimeError('startup failed')
        self.assertIsNotNone(child.poll())

    def test_occupied_port_does_not_reuse_existing_service(self):
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            with self.assertRaisesRegex(stack.SetupError, 'in use'):
                stack.check_ports({**stack.DEFAULTS, 'http_port': listener.getsockname()[1]})

    def test_release_layout_preserves_launcher_after_extraction(self):
        self.marker()
        stack.install_launchers(self.prefix)
        result = subprocess.run([str(self.prefix / 'run.sh'), '--help'], cwd=self.root,
                                capture_output=True, text=True)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertIn('--prefix', result.stdout)

    def test_self_signed_certificate_uses_localhost_root_trust(self):
        self.marker()
        (self.prefix / 'tls').mkdir()
        commands = []
        with patch.object(stack, 'checked', side_effect=lambda command, **options: commands.append(command)), \
                patch.object(stack.platform, 'system', return_value='Darwin'):
            stack.create_certificates(self.prefix, {'certificate': 'test-password'}, stack.read_marker(self.prefix))
        trust = commands[-1]
        self.assertEqual(['security', 'add-trusted-cert', '-r', 'trustRoot', '-p', 'ssl', '-s', 'localhost'], trust[:8])
        self.assertTrue(stack.read_marker(self.prefix)['certificate_trust_requested'])

    def test_certificate_trust_removed_before_files(self):
        self.marker()
        marker = stack.read_marker(self.prefix)
        marker['certificate_trust_requested'] = True
        stack.write_json(self.prefix / stack.MARKER, marker)
        (self.prefix / 'tls').mkdir()
        certificate = self.prefix / 'tls/localhost.crt'
        certificate.write_text(ssl_certificate())
        commands = []
        with patch.object(stack, 'checked', side_effect=lambda command: commands.append(command)), \
                patch.object(stack, 'capture', side_effect=[(0, '', ''), (0, 'SHA-1 hash: ' + hashlib.sha1(b'fixture certificate').hexdigest().upper(), '')]) as capture:
            stack.uninstall(argparse.Namespace(prefix=str(self.prefix), yes=True))
        self.assertEqual('remove-trusted-cert', capture.call_args_list[0][0][0][1])
        self.assertEqual('delete-certificate', commands[0][1])
        self.assertFalse(self.prefix.exists())

    def test_failed_or_already_removed_certificate_does_not_block_cleanup(self):
        self.marker()
        marker = stack.read_marker(self.prefix)
        marker['certificate_trust_requested'] = True
        stack.write_json(self.prefix / stack.MARKER, marker)
        (self.prefix / 'tls').mkdir()
        (self.prefix / 'tls/localhost.crt').write_text(ssl_certificate())
        with patch.object(stack, 'capture', side_effect=[
                (1, '', 'The specified item could not be found in the keychain.'), (0, '', '')]):
            stack.uninstall(argparse.Namespace(prefix=str(self.prefix), yes=True))
        self.assertFalse(self.prefix.exists())

    def test_keychain_authorization_error_preserves_install_for_retry(self):
        self.marker()
        marker = stack.read_marker(self.prefix)
        marker['certificate_trust_requested'] = True
        stack.write_json(self.prefix / stack.MARKER, marker)
        (self.prefix / 'tls').mkdir()
        (self.prefix / 'tls/localhost.crt').write_text(ssl_certificate())
        with patch.object(stack, 'capture', return_value=(1, '', 'User interaction is not allowed.')):
            with self.assertRaisesRegex(stack.SetupError, 'certificate trust'):
                stack.uninstall(argparse.Namespace(prefix=str(self.prefix), yes=True))
        self.assertTrue(stack.read_marker(self.prefix)['certificate_trust_requested'])


    def fixture_install(self):
        self.marker()
        for directory in ('runtime', 'proxy', 'bin', 'tls', 'logs', 'cache', 'models', 'runtime-home'):
            (self.prefix / directory).mkdir()
        with __import__('contextlib').ExitStack() as scope:
            sockets = [scope.enter_context(socket.socket()) for _ in stack.PORT_KEYS]
            for listener in sockets:
                listener.bind(('127.0.0.1', 0))
            config = {**stack.DEFAULTS, **dict(zip(stack.PORT_KEYS,
                      [listener.getsockname()[1] for listener in sockets]))}
        stack.write_json(self.prefix / 'config.json', config)
        keys = {'public': 'test-public-key', 'runtime': 'test-runtime-key', 'certificate': 'test-password'}
        stack.write_json(self.prefix / 'keys.json', keys)
        (self.prefix / 'tls/openssl.cnf').write_text(
            '[req]\ndistinguished_name=dn\n[dn]\n[localhost]\n'
            'subjectAltName=DNS:localhost,IP:127.0.0.1\n'
            'basicConstraints=critical,CA:FALSE\nextendedKeyUsage=serverAuth\n')
        with patch.object(stack.platform, 'system', return_value='Linux'):
            stack.create_certificates(self.prefix, keys, stack.read_marker(self.prefix))
        wrapper = '#!' + sys.executable + '\nimport os\nos.execv(' + repr(sys.executable) + ', [' +             repr(sys.executable) + ', ' + repr(str(Path(__file__).resolve())) + ', "--fixture-service"])\n'
        for executable in ('runtime/LlamaRuntime.Presentation.Grpc', 'proxy/GenProxy.Api.Host', 'bin/rss-sum'):
            path = self.prefix / executable
            path.write_text(wrapper)
            path.chmod(0o755)
        stack.install_launchers(self.prefix)
        return config

    def run_fixture(self, interrupt_backend=False):
        config = self.fixture_install()
        with subprocess.Popen([str(self.prefix / 'run.sh')], stdout=subprocess.PIPE,
                              stderr=subprocess.STDOUT, text=True) as owner:
            output = queue.Queue()
            def read_output():
                for line in owner.stdout:
                    output.put(line)
                output.put(None)
            reader = threading.Thread(target=read_output)
            reader.start()
            try:
                while True:
                    line = output.get(timeout=20)
                    self.assertIsNotNone(line, 'Launcher exited before app startup')
                    if 'FIXTURE_APP_STARTED' in line:
                        break
                for name in ('generation', 'summarizer', 'proxy', 'app'):
                    self.assertTrue((self.prefix / f'logs/{name}.started').exists(), name)
                proxy_env = json.loads((self.prefix / 'logs/proxy.env').read_text())
                self.assertEqual('00:05:00', proxy_env['GenerationRuntime__Timeouts__Generate'])
                self.assertEqual('00:05:00', proxy_env['PromptReducerRuntime__Timeouts__Generate'])
                generation_env = json.loads((self.prefix / 'logs/generation.env').read_text())
                summarizer_env = json.loads((self.prefix / 'logs/summarizer.env').read_text())
                self.assertEqual('4096', generation_env['Llama__Native__ContextSize'])
                self.assertEqual('8192', summarizer_env['Llama__Native__ContextSize'])
                self.assertEqual(generation_env['HostedModel__ModelPath'], summarizer_env['HostedModel__ModelPath'])
                self.assertEqual('test-runtime-key', proxy_env['GenerationRuntime__ApiKey'])
                if interrupt_backend:
                    os.kill(int((self.prefix / 'logs/generation.started').read_text()), signal.SIGTERM)
                else:
                    owner.send_signal(signal.SIGTERM)
                owner.wait(timeout=20)
                self.assertEqual(1 if interrupt_backend else 130, owner.returncode)
                for name in ('generation', 'summarizer', 'proxy', 'app'):
                    self.assertTrue((self.prefix / f'logs/{name}.stopped').exists(), name)
            finally:
                if owner.poll() is None:
                    owner.send_signal(signal.SIGTERM)
                    owner.wait(timeout=20)
                reader.join(timeout=5)
                owner.stdout.close()
        stack.uninstall(argparse.Namespace(prefix=str(self.prefix), yes=True))
        self.assertFalse(self.prefix.exists())

    def test_launcher_stops_every_service_on_sigterm_then_uninstalls(self):
        self.run_fixture()

    def test_backend_failure_stops_app_proxy_and_other_backend(self):
        self.run_fixture(interrupt_backend=True)



def ssl_certificate():
    import ssl
    return ssl.DER_cert_to_PEM_cert(b'fixture certificate')



def fixture_service():
    cwd = Path.cwd()
    if 'HostedModel__ModelPath' in os.environ:
        prefix = cwd.parent
        config = json.loads((prefix / 'config.json').read_text())
        port = int(os.environ['Kestrel__Endpoints__Grpc__Url'].rsplit(':', 1)[1])
        name = 'generation' if port == config['generation_port'] else 'summarizer'
    elif 'GenerationRuntime__Address' in os.environ:
        prefix = cwd.parent
        port = int(os.environ['ASPNETCORE_URLS'].rsplit(':', 1)[1])
        name = 'proxy'
    else:
        prefix = cwd
        name = 'app'
    log = prefix / 'logs'
    (log / f'{name}.env').write_text(json.dumps(dict(os.environ)))
    def stop(_signal, _frame):
        (log / f'{name}.stopped').write_text('stopped')
        sys.exit(0)
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    (log / f'{name}.started').write_text(str(os.getpid()))
    if name == 'app':
        print('FIXTURE_APP_STARTED', flush=True)
        while True:
            signal.pause()
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b'Healthy')
        def do_POST(self):
            self.send_response(400 if self.headers.get('X-API-Key') == 'test-public-key' else 401)
            self.end_headers()
        def log_message(self, *args):
            pass
    server = HTTPServer(('localhost', port), Handler)
    if name != 'proxy':
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(str(prefix / 'tls/localhost.crt'), str(prefix / 'tls/localhost.key'))
        server.socket = context.wrap_socket(server.socket, server_side=True)
    server.serve_forever()


if __name__ == '__main__':
    if '--fixture-service' in sys.argv:
        fixture_service()
    else:
        unittest.main()
