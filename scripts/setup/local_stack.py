#!/usr/bin/env python3
"""Provision and run a private RSS Sum installation using only Python's stdlib."""

import argparse
from contextlib import contextmanager, ExitStack
import fcntl
import secrets
import ssl
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import socket
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
import urllib.parse

GEN_PROXY_REVISION = '6526533f948e00d2312a3a63e943782058f795c9'
RUNTIME_REVISION = '09a132c1d6370e0ffb8b426b6eca8c8f8da94205'
SOURCE_HASHES = {
    'gen-proxy': '1776f6fa8d498f775d631b4dfef3903ecdefb9ba74f36d5af337ae92fe4a900b',
    'llama-runtime': '2236f589977dc41c7b386addf346051a5bd9845fa2a3ace2895e1e5795258650',
}
BACKEND_RELEASES = {
    ('Darwin', 'arm64'): {
        'gen-proxy': (
            'https://github.com/rjxby/gen-proxy/releases/download/v0.5.0/gen-proxy-osx-arm64',
            '429785e9b4c0f640273eea76136383a2097282475e69919b342b924af11b8607'),
        'llama-runtime': (
            'https://github.com/rjxby/llama-runtime/releases/download/v0.7.1/llama-runtime-grpc-osx-arm64.tar.gz',
            'a46eddf80b44c2de24a08052864e4839f4820a1613c60e78f46c7b1a6ed44fef'),
    },
}
DEFAULTS = {
    'http_port': 8080,
    'proxy_port': 7001,
    'generation_port': 50051,
    'summarizer_port': 50052,
    'model_url': 'https://huggingface.co/bartowski/Llama-3.2-1B-Instruct-GGUF/resolve/'
                 '067b946cf014b7c697f3654f621d577a3e3afd1c/Llama-3.2-1B-Instruct-Q4_K_M.gguf',
    'model_sha256': '6f85a640a97cf2bf5b8e764087b1e83da0fdb51d7c9fab7d0fece9385611df83',
    'model_id': 'llama-3.2-1b',
    'feeds': 'https://go.dev/blog/feed.atom',
    'item_limit': 3,
    'interval': 3600,
    'timeout': 300,
    'generation_context': 4096,
    'summarizer_context': 8192,
}
PORT_KEYS = ('http_port', 'proxy_port', 'generation_port', 'summarizer_port')
MARKER = '.rss-sum-install.json'
DEFAULT_PREFIX = Path.home() / '.local/share/rss-sum'
REPOSITORY = Path(__file__).resolve().parents[2]


class SetupError(Exception):
    pass


def installation_path(value):
    path = Path(value).expanduser().absolute()
    if path != path.resolve():
        raise SetupError('Install directory must not contain symlinks or .. components.')
    if path in (Path('/'), Path.home(), REPOSITORY) or path in REPOSITORY.parents:
        raise SetupError('Choose a dedicated install directory, separate from your home or checkout.')
    return path


def validate_config(config):
    if not isinstance(config, dict) or set(config) != set(DEFAULTS):
        raise SetupError('config.json must contain exactly the documented setup settings.')
    for key in PORT_KEYS + ('item_limit', 'interval', 'timeout', 'generation_context', 'summarizer_context'):
        if type(config[key]) is not int or config[key] < 1:
            raise SetupError(f'{key} must be a positive integer.')
    if any(config[key] > 65536 for key in ('generation_context', 'summarizer_context')):
        raise SetupError('Runtime contexts must not exceed 65536 tokens.')
    if config['generation_context'] <= 512 or config['summarizer_context'] <= config['generation_context']:
        raise SetupError('Generation context must exceed 512 tokens; summarizer context must be larger.')
    if config['timeout'] > 86400:
        raise SetupError('Summary timeout must not exceed 86400 seconds.')
    for key in PORT_KEYS:
        if config[key] > 65535:
            raise SetupError(f'{key} must be between 1 and 65535.')
    if len({config[key] for key in PORT_KEYS}) != len(PORT_KEYS):
        raise SetupError('The web, proxy, generation, and summarizer ports must differ.')
    for key in ('model_url', 'model_sha256', 'model_id'):
        if not isinstance(config[key], str):
            raise SetupError(f'{key} must be a string.')
    url = urllib.parse.urlsplit(config['model_url'])
    if url.scheme != 'https' or url.hostname != 'huggingface.co' or not url.path.endswith('.gguf'):
        raise SetupError('Model URL must be an HTTPS Hugging Face GGUF download URL.')
    if not re.fullmatch(r'[a-fA-F0-9]{64}', config['model_sha256']):
        raise SetupError('Model SHA-256 must contain 64 hexadecimal characters.')
    if not re.fullmatch(r'[A-Za-z0-9_.-]{1,128}', config['model_id']):
        raise SetupError('Model ID must contain 1 to 128 letters, numbers, dots, underscores, or hyphens.')
    feeds = config['feeds']
    if not isinstance(feeds, str) or not feeds or any(c.isspace() for c in feeds):
        raise SetupError('Feeds must be comma-separated public HTTP(S) URLs without whitespace.')
    for feed in feeds.split(','):
        parsed = urllib.parse.urlsplit(feed)
        if parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username or parsed.password:
            raise SetupError('Feeds must be public HTTP(S) URLs without credentials.')
    # The app performs DNS, redirect, and destination validation when it fetches feeds.
    return config


def configuration(args):
    config = dict(DEFAULTS)
    for key in config:
        value = getattr(args, key, None)
        if value is not None:
            config[key] = value
    if not args.defaults:
        if not sys.stdin.isatty():
            raise SetupError('Interactive setup requires a terminal. Use --defaults for unattended setup.')
        print('RSS Sum local setup. Press Enter to accept each value.')
        for key, label in (
                ('http_port', 'Web port'), ('proxy_port', 'Gen-proxy port'),
                ('generation_port', 'Generation gRPC port'), ('summarizer_port', 'Summarizer gRPC port'),
                ('model_url', 'Hugging Face GGUF download URL'), ('model_sha256', 'Model SHA-256'),
                ('model_id', 'Model ID'), ('feeds', 'Feed URLs, comma-separated'),
                ('item_limit', 'Items per feed'), ('interval', 'Poll interval in seconds'),
                ('timeout', 'Summary timeout in seconds'),
                ('generation_context', 'Generation context in tokens'),
                ('summarizer_context', 'Prompt-reducer context in tokens')):
            answer = input(f'{label} [{config[key]}]: ').strip()
            if answer:
                try:
                    config[key] = int(answer) if isinstance(DEFAULTS[key], int) else answer
                except ValueError as error:
                    raise SetupError(f'{label} must be an integer.') from error
    return validate_config(config)


def write_json(path, value):
    with tempfile.NamedTemporaryFile(mode='w', dir=path.parent, delete=False) as temporary:
        temporary.write(json.dumps(value, indent=2) + '\n')
        temporary_path = Path(temporary.name)
    try:
        temporary_path.replace(path)
    finally:
        temporary_path.unlink(missing_ok=True)


def read_marker(prefix):
    marker = prefix / MARKER
    if marker.is_symlink():
        raise SetupError('Install marker must not be a symlink.')
    try:
        data = json.loads(marker.read_text())
    except (OSError, ValueError) as error:
        raise SetupError(f'No valid rss-sum install marker in {prefix}. Refusing to remove or run it.') from error
    if data.get('schema') != 1 or data.get('prefix') != str(prefix):
        raise SetupError('Install marker does not match this directory.')
    return data


@contextmanager
def install_lock(prefix):
    path = prefix / '.lock'
    descriptor = os.open(path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as handle:
        try:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise SetupError('This installation is busy. Stop run.sh with Ctrl+C before setup or uninstall.') from error
        yield


def check_ports(config):
    for key in PORT_KEYS:
        with socket.socket() as listener:
            try:
                listener.bind(('127.0.0.1', config[key]))
            except OSError as error:
                raise SetupError(f'Port {config[key]} is in use. Choose another {key}.') from error


def download(url, destination, expected_hash):
    print(f'Downloading {url}', flush=True)
    digest = hashlib.sha256()
    request = urllib.request.Request(url, headers={'User-Agent': 'rss-sum-setup'})
    with urllib.request.urlopen(request, timeout=60) as response, destination.open('wb') as output:
        while True:
            chunk = response.read(1024 * 1024)
            if not chunk:
                break
            output.write(chunk)
            digest.update(chunk)
    if digest.hexdigest() != expected_hash.lower():
        raise SetupError('Download failed SHA-256 verification.')


def within(path, root):
    try:
        path.resolve().relative_to(root.resolve())
    except ValueError as error:
        raise SetupError('Runtime archive contains a path outside its install directory.') from error


def extract_members(archive, destination):
    for member in archive:
        target = destination / member.name
        within(target, destination)
        if target.is_symlink():
            raise SetupError('Runtime archive attempts to overwrite a symlink.')
        target.parent.mkdir(parents=True, exist_ok=True)
        if member.isdir():
            target.mkdir(exist_ok=True)
        elif member.isfile():
            with archive.extractfile(member) as source, target.open('wb') as output:
                shutil.copyfileobj(source, output)
            target.chmod(member.mode & 0o755)
        elif member.issym():
            within(target.parent / member.linkname, destination)
            target.symlink_to(member.linkname)
        elif member.islnk():
            source = destination / member.linkname
            within(source, destination)
            os.link(source, target)
        else:
            raise SetupError('Runtime archive contains an unsupported file type.')


def extract_runtime(archive_path, destination):
    with tarfile.open(archive_path, mode='r:gz') as archive:
        extract_members(archive, destination)


@contextmanager
def managed_process(command, **options):
    process = subprocess.Popen(command, start_new_session=True, **options)
    try:
        yield process
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()


def stack_environment(prefix):
    blocked = ('ASPNETCORE_', 'DOTNET_', 'Kestrel__', 'HostedModel__', 'Llama__', 'Inference__',
               'ApiKeys__', 'GenerationRuntime__', 'PromptReducerRuntime__', 'Timeouts__', 'ResponsesTimeout__')
    env = {key: value for key, value in os.environ.items()
           if not key.lower().startswith(tuple(item.lower() for item in blocked))}
    env.update(HOME=str(prefix / 'runtime-home'), DOTNET_BUNDLE_EXTRACT_BASE_DIR=str(prefix / 'cache'),
               ASPNETCORE_ENVIRONMENT='Production', SSL_CERT_FILE=str(prefix / 'tls/localhost.crt'))
    return env


def app_environment(prefix, config):
    env = dict(os.environ)
    env.update(RUN_MIGRATION='true', HTTP_SERVER_ENABLED='true', RSS_WORKER_ENABLED='true',
               HTTP_ADDR=f"127.0.0.1:{config['http_port']}", DATABASE_PATH='data/rss-sum.sqlite',
               FEEDS=config['feeds'], FEED_ITEMS_LIMIT=str(config['item_limit']),
               WORKER_INTERVAL_IN_SECONDS=str(config['interval']), WORKER_TIMEOUT_IN_SECONDS='1800',
               LLM_SYSTEM_PROMPT_FILE='',
               GEN_PROXY_BASE_URL=f"http://127.0.0.1:{config['proxy_port']}",
               GEN_PROXY_MODEL=config['model_id'],
               GEN_PROXY_API_KEY=json.loads((prefix / 'keys.json').read_text())['public'],
               GEN_PROXY_TIMEOUT_IN_SECONDS=str(config['timeout']))
    return env


def ensure_alive(processes):
    for name, process in processes:
        if process.poll() is not None:
            raise SetupError(f'{name} exited with status {process.returncode}. See logs/{name}.log.')


def wait_ready(url, processes, context=None, timeout=180):
    deadline = time.monotonic() + timeout
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                        urllib.request.HTTPSHandler(context=context))
    while time.monotonic() < deadline:
        ensure_alive(processes)
        try:
            with opener.open(url, timeout=1) as response:
                if response.status == 200:
                    ensure_alive(processes)
                    return
        except (OSError, ValueError):
            pass
        time.sleep(0.1)
    raise SetupError(f'Stack did not become ready in {timeout} seconds. See logs/.')


@contextmanager
def local_stack(prefix, config):
    keys = json.loads((prefix / 'keys.json').read_text())
    context = ssl.create_default_context(cafile=str(prefix / 'tls/localhost.crt'))
    processes = []
    with ExitStack() as scope:
        for name, port in (('generation', config['generation_port']), ('summarizer', config['summarizer_port'])):
            env = stack_environment(prefix)
            env.update(HostedModel__ModelPath=str(prefix / 'models/model.gguf'),
                       HostedModel__ModelId=config['model_id'], ApiKeys__Keys__0=keys['runtime'],
                       Inference__WorkerCount='1', Llama__Native__ContextSize=str(config[f'{name}_context']),
                       Llama__Native__GenerationMaxNewTokens='512')
            env['Llama__Native__NativeLibraryPath'] = str(prefix / 'runtime' / (
                'libllama_adapter.dylib' if platform.system() == 'Darwin' else 'libllama_adapter.so'))
            env['Kestrel__Endpoints__Grpc__Url'] = f'https://localhost:{port}'
            env['Kestrel__Endpoints__Grpc__Protocols'] = 'Http1AndHttp2'
            env['Kestrel__Endpoints__Grpc__Certificate__Path'] = str(prefix / 'tls/localhost.pfx')
            env['Kestrel__Endpoints__Grpc__Certificate__Password'] = keys['certificate']
            log = scope.enter_context((prefix / f'logs/{name}.log').open('a'))
            process = scope.enter_context(managed_process(
                [str(prefix / 'runtime/LlamaRuntime.Presentation.Grpc')], env=env,
                cwd=prefix / 'runtime', stdout=log, stderr=subprocess.STDOUT))
            processes.append((name, process))
            wait_ready(f'https://localhost:{port}/health/ready', processes, context)
        env = stack_environment(prefix)
        env.update(ASPNETCORE_URLS=f"http://127.0.0.1:{config['proxy_port']}",
                   ApiKeys__Keys__0=keys['public'],
                   GenerationRuntime__Address=f"https://localhost:{config['generation_port']}",
                   GenerationRuntime__ApiKey=keys['runtime'], PromptReducerRuntime__Enabled='true',
                   PromptReducerRuntime__Address=f"https://localhost:{config['summarizer_port']}",
                   PromptReducerRuntime__ApiKey=keys['runtime'],
                   GenerationRuntime__Timeouts__Generate=duration(config['timeout']),
                   PromptReducerRuntime__Timeouts__Generate=duration(config['timeout']),
                   ResponsesTimeout__Timeout=duration(config['timeout']))
        log = scope.enter_context((prefix / 'logs/proxy.log').open('a'))
        proxy = scope.enter_context(managed_process([str(prefix / 'proxy/GenProxy.Api.Host')],
                                                   env=env, cwd=prefix / 'proxy', stdout=log,
                                                   stderr=subprocess.STDOUT))
        processes.append(('proxy', proxy))
        # An authenticated validation error proves the Responses endpoint is ready without inference.
        wait_proxy(prefix, config, processes)
        yield processes


def duration(seconds):
    days, remaining = divmod(seconds, 86400)
    hours, remaining = divmod(remaining, 3600)
    minutes, seconds = divmod(remaining, 60)
    day_prefix = f'{days}.' if days else ''
    return f'{day_prefix}{hours:02}:{minutes:02}:{seconds:02}'


def wait_proxy(prefix, config, processes):
    key = json.loads((prefix / 'keys.json').read_text())['public']
    request = urllib.request.Request(f"http://127.0.0.1:{config['proxy_port']}/v1/responses",
                                     data=b'{}', headers={'Content-Type': 'application/json', 'X-API-Key': key})
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        ensure_alive(processes)
        try:
            with opener.open(request, timeout=1):
                pass
        except urllib.error.HTTPError as error:
            if error.code == 400:
                ensure_alive(processes)
                return
        except OSError:
            pass
        time.sleep(0.1)
    raise SetupError('Gen-proxy did not become ready. See logs/proxy.log.')


def checked(command, **kwargs):
    with managed_process(command, **kwargs) as process:
        if process.wait() != 0:
            raise SetupError(f'{Path(command[0]).name} failed. See the command output above.')


def create_certificates(prefix, keys, marker):
    tls = prefix / 'tls'
    checked(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '3650',
             '-subj', '/CN=localhost', '-keyout', str(tls / 'localhost.key'),
             '-out', str(tls / 'localhost.crt'), '-config', str(tls / 'openssl.cnf'),
             '-extensions', 'localhost'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    env = {**os.environ, 'RSS_SUM_CERT_PASSWORD': keys['certificate']}
    checked(['openssl', 'pkcs12', '-export', '-out', str(tls / 'localhost.pfx'),
             '-inkey', str(tls / 'localhost.key'), '-in', str(tls / 'localhost.crt'),
             '-passout', 'env:RSS_SUM_CERT_PASSWORD'], env=env)
    if platform.system() == 'Darwin':
        marker['certificate_trust_requested'] = True
        write_json(prefix / MARKER, marker)
        print('Trusting the private gRPC certificate for localhost in your login keychain.', flush=True)
        checked(['security', 'add-trusted-cert', '-r', 'trustRoot', '-p', 'ssl', '-s', 'localhost',
                 '-k', str(Path.home() / 'Library/Keychains/login.keychain-db'), str(tls / 'localhost.crt')])


def copy_application(prefix, supplied):
    binary = Path(supplied).expanduser().resolve() if supplied else REPOSITORY / 'rss-sum'
    destination = prefix / 'bin/rss-sum'
    if binary.is_file():
        shutil.copy2(binary, destination)
    elif supplied:
        raise SetupError(f'RSS Sum binary not found: {binary}')
    elif (REPOSITORY / 'go.mod').is_file():
        print('Building rss-sum from this checkout.', flush=True)
        with managed_process(['go', 'build', '-trimpath', '-o', str(destination), '.'],
                             cwd=REPOSITORY) as process:
            if process.wait() != 0:
                raise SetupError('RSS Sum build failed. Install the Go version in go.mod and a C compiler.')
    else:
        raise SetupError('Extract the complete release archive, or pass --binary /path/to/rss-sum.')
    destination.chmod(0o755)


def install_launchers(prefix):
    shutil.copy2(__file__, prefix / 'local_stack.py')
    for name, command in (('run.sh', 'run'), ('uninstall.sh', 'uninstall')):
        (prefix / name).write_text(
            '#!/bin/sh\nset -eu\n'
            'SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)\n'
            f'exec python3 "$SCRIPT_DIR/local_stack.py" {command} --prefix "$SCRIPT_DIR" "$@"\n')
        (prefix / name).chmod(0o755)


def install_dependencies(prefix, bundle, build_backends=False):
    if bundle:
        source = Path(bundle).expanduser().resolve()
        if not source.is_dir():
            raise SetupError(f'Local stack bundle not found: {source}')
        metadata = json.loads((source / 'versions.json').read_text())
        expected = {'gen_proxy': GEN_PROXY_REVISION, 'llama_runtime': RUNTIME_REVISION,
                    'platform': f'{platform.system()}/{platform.machine()}'}
        if metadata != expected:
            raise SetupError('Local stack bundle versions or platform do not match this installer.')
        for name in ('proxy', 'runtime'):
            shutil.copytree(source / name, prefix / name, dirs_exist_ok=True, symlinks=True)
    elif build_backends:
        build_dependencies(prefix)
    else:
        download_dependencies(prefix)
    for binary in (prefix / 'proxy/GenProxy.Api.Host', prefix / 'runtime/LlamaRuntime.Presentation.Grpc'):
        if not binary.is_file():
            raise SetupError(f'Incomplete local stack bundle: {binary.name} is missing.')
        binary.chmod(0o755)


def backend_releases():
    system = (platform.system(), platform.machine())
    releases = BACKEND_RELEASES.get(system)
    if releases is None:
        raise SetupError('Prebuilt backends are available only for macOS ARM64. '
                         'Use --build-backends with source build tools, or --stack-bundle.')
    return releases


def download_dependencies(destination):
    releases = backend_releases()
    download(releases['gen-proxy'][0], destination / 'proxy/GenProxy.Api.Host', releases['gen-proxy'][1])
    with tempfile.TemporaryDirectory(prefix='rss-sum-backend-download-') as temporary:
        archive = Path(temporary) / 'runtime.tar.gz'
        download(releases['llama-runtime'][0], archive, releases['llama-runtime'][1])
        extract_runtime(archive, destination / 'runtime')
    write_json(destination / 'versions.json', {'gen_proxy': GEN_PROXY_REVISION,
               'llama_runtime': RUNTIME_REVISION, 'platform': f'{platform.system()}/{platform.machine()}'})


def build_dependencies(destination):
    system = (platform.system(), platform.machine())
    target = {('Darwin', 'arm64'): ('macos-arm64', 'osx-arm64'),
              ('Linux', 'x86_64'): ('ubuntu-x64', 'linux-x64')}.get(system)
    if target is None:
        raise SetupError('Local setup supports Linux AMD64 and macOS ARM64.')
    for tool in ('dotnet', 'cmake', 'make', 'unzip', 'shasum'):
        if not shutil.which(tool):
            raise SetupError(f'Building backends needs {tool}. Use prebuilt backends or --stack-bundle to avoid build tools.')
    with tempfile.TemporaryDirectory(prefix='rss-sum-stack-build-') as temporary:
        root = Path(temporary)
        for repo, revision in (('gen-proxy', GEN_PROXY_REVISION), ('llama-runtime', RUNTIME_REVISION)):
            archive = root / f'{repo}.tar.gz'
            download(f'https://codeload.github.com/rjxby/{repo}/tar.gz/{revision}', archive, SOURCE_HASHES[repo])
            extract_runtime(archive, root)
        proxy = root / f'gen-proxy-{GEN_PROXY_REVISION}'
        runtime = root / f'llama-runtime-{RUNTIME_REVISION}'
        (runtime / '.env').write_text(f'PLATFORM={target[0]}\nLLAMA_VERSION=b10964\nDOTNET_RUNTIME={target[1]}\n')
        clean_env = {key: value for key, value in os.environ.items() if key not in ('PLATFORM', 'GOOS', 'GOARCH')}
        checked(['make', 'init'], cwd=runtime, env=clean_env)
        checked(['make', 'pack'], cwd=runtime, env=clean_env)
        shutil.copytree(runtime / 'dist', destination / 'runtime', dirs_exist_ok=True, symlinks=True)
        checked(['dotnet', 'publish', 'Backend/Api/Host/GenProxy.Api.Host.csproj', '-c', 'Release',
                 '-r', target[1], '--self-contained', 'true', '-p:PublishSingleFile=true',
                 '-p:EnableCompressionInSingleFile=true', '-p:DebugType=None', '-p:DebugSymbols=false',
                 '-o', str(destination / 'proxy')], cwd=proxy, env=clean_env)
        shutil.copy2(proxy / 'LICENSE', destination / 'proxy/LICENSE-gen-proxy')
    write_json(destination / 'versions.json', {'gen_proxy': GEN_PROXY_REVISION,
               'llama_runtime': RUNTIME_REVISION, 'platform': f'{system[0]}/{system[1]}'})


def confirm_downloads(args, config, prefix):
    print(f'Install RSS Sum in {prefix}.')
    if args.stack_bundle:
        print(f'Copy backends from {args.stack_bundle}.')
    elif args.build_backends:
        print('Download pinned gen-proxy and llama-runtime sources from GitHub and build locally. '
              'The build also downloads llama.cpp and .NET dependencies.')
    else:
        print('Download pinned gen-proxy v0.5.0 and llama-runtime v0.7.1 binaries from GitHub.')
    print(f'Download the GGUF model from {config["model_url"]}.')
    print('Setup verifies download SHA-256 checksums and prepares a private localhost certificate.')
    if platform.system() == 'Darwin':
        print('Setup adds the localhost certificate to your login keychain.')
    if args.accept_downloads:
        return True
    if not sys.stdin.isatty():
        raise SetupError('Download consent requires a terminal or --accept-downloads. '
                         '--defaults selects configuration only.')
    try:
        approved = input('Download and install these dependencies? [y/N]: ').strip().lower() in ('y', 'yes')
    except EOFError:
        approved = False
    if not approved:
        print('Setup cancelled. No files or downloads created.')
    return approved


def install(args):
    prefix = installation_path(args.prefix)
    config = configuration(args)
    if (platform.system(), platform.machine()) not in (('Darwin', 'arm64'), ('Linux', 'x86_64')):
        raise SetupError('Local setup supports Linux AMD64 and macOS ARM64, matching release archives.')
    if not shutil.which('openssl'):
        raise SetupError('Install OpenSSL before setup to create the private gRPC certificate.')
    check_ports(config)
    if prefix.exists():
        raise SetupError(f'{prefix} already exists. Choose another --prefix or uninstall it first.')
    if not confirm_downloads(args, config, prefix):
        return
    if not args.stack_bundle and not args.build_backends:
        backend_releases()
    prefix.mkdir(parents=True, mode=0o700)
    marker = {'schema': 1, 'prefix': str(prefix), 'complete': False,
              'certificate_trust_requested': False}
    write_json(prefix / MARKER, marker)
    with install_lock(prefix):
        for name in ('bin', 'proxy', 'runtime', 'runtime-home', 'cache', 'models', 'data', 'logs', 'tls'):
            (prefix / name).mkdir()
        install_launchers(prefix)
        write_json(prefix / 'config.json', config)
        try:
            copy_application(prefix, args.binary)
            install_dependencies(prefix, args.stack_bundle, args.build_backends)
            print('Downloading the GGUF model for generation and prompt reduction.', flush=True)
            download(config['model_url'], prefix / 'models/model.gguf', config['model_sha256'])
            keys = {name: secrets.token_hex(24) for name in ('public', 'runtime', 'certificate')}
            write_json(prefix / 'keys.json', keys)
            (prefix / 'keys.json').chmod(0o600)
            (prefix / 'tls/openssl.cnf').write_text(
                '[req]\ndistinguished_name=dn\n[dn]\n[localhost]\n'
                'subjectAltName=DNS:localhost,IP:127.0.0.1,IP:::1\n'
                'basicConstraints=critical,CA:FALSE\nextendedKeyUsage=serverAuth\n')
            create_certificates(prefix, keys, marker)
            with local_stack(prefix, config):
                print('Both gRPC backends and gen-proxy are ready.', flush=True)
            marker['complete'] = True
            write_json(prefix / MARKER, marker)
        except BaseException:
            print(f'Incomplete setup kept at {prefix}. Clean up with ./uninstall.sh --prefix "{prefix}" --yes.',
                  file=sys.stderr)
            raise
    print(f'Installed RSS Sum at {prefix}\nStart: {prefix / "run.sh"}\nWeb: http://127.0.0.1:{config["http_port"]}')


def run(args):
    prefix = installation_path(args.prefix)
    if not read_marker(prefix).get('complete'):
        raise SetupError('Setup is incomplete. Uninstall this directory and run setup again.')
    with install_lock(prefix):
        config = validate_config(json.loads((prefix / 'config.json').read_text()))
        check_ports(config)
        with local_stack(prefix, config) as processes, managed_process(
                [str(prefix / 'bin/rss-sum')], cwd=prefix, env=app_environment(prefix, config)) as app:
            print(f"RSS Sum at http://127.0.0.1:{config['http_port']}. Ctrl+C stops the stack.", flush=True)
            while app.poll() is None:
                ensure_alive(processes)
                time.sleep(0.1)
            if app.returncode != 0:
                raise SetupError(f'RSS Sum exited with status {app.returncode}.')


def capture(command):
    with managed_process(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True) as process:
        stdout, stderr = process.communicate(timeout=30)
        return process.returncode, stdout, stderr


def remove_certificate(prefix):
    certificate = prefix / 'tls/localhost.crt'
    fingerprint = hashlib.sha1(ssl.PEM_cert_to_DER_cert(certificate.read_text())).hexdigest().upper()
    status, _, error = capture(['security', 'remove-trusted-cert', str(certificate)])
    absent = any(message in error.lower() for message in
                 ('-25300', 'specified item could not be found', 'no trust settings'))
    if status != 0 and not absent:
        raise SetupError(f'Could not remove localhost certificate trust: {error.strip()}')
    keychain = str(Path.home() / 'Library/Keychains/login.keychain-db')
    status, certificates, error = capture(['security', 'find-certificate', '-a', '-Z', keychain])
    if status != 0:
        raise SetupError(f'Could not inspect login keychain certificates: {error.strip()}')
    if f'SHA-1 hash: {fingerprint}' in certificates:
        checked(['security', 'delete-certificate', '-Z', fingerprint, keychain])


def uninstall(args):
    prefix = installation_path(args.prefix)
    if not prefix.exists():
        print(f'Nothing installed at {prefix}.')
        return
    marker = read_marker(prefix)
    with install_lock(prefix):
        print(f'Remove {prefix}, including the database, downloaded models, runtime, config, and logs?')
        if not args.yes:
            if not sys.stdin.isatty():
                raise SetupError('Uninstall requires a terminal confirmation or --yes.')
            if input('Type remove to continue: ').strip() != 'remove':
                print('Uninstall cancelled.')
                return
        if marker.get('certificate_trust_requested'):
            remove_certificate(prefix)
            marker['certificate_trust_requested'] = False
            write_json(prefix / MARKER, marker)
        shutil.rmtree(prefix)
    print(f'Removed {prefix}.')


def parser():
    root = argparse.ArgumentParser(description='Install, run, or remove the local RSS Sum stack.')
    commands = root.add_subparsers(dest='command', required=True)
    setup = commands.add_parser('install', help='Provision gen-proxy, gRPC runtimes, and a Hugging Face model.')
    setup.add_argument('--defaults', action='store_true', help='Use configuration defaults; download consent is still required.')
    setup.add_argument('--accept-downloads', action='store_true', help='Consent to dependency/model downloads and local setup.')
    backends = setup.add_mutually_exclusive_group()
    backends.add_argument('--stack-bundle', help='Copy backends from a prebuilt local stack directory.')
    backends.add_argument('--build-backends', action='store_true', help='Download and build pinned backend sources instead of binaries.')
    setup.add_argument('--binary', help='RSS Sum executable to install; release bundles include it.')
    for key, value in DEFAULTS.items():
        setup.add_argument('--' + key.replace('_', '-'), type=type(value), help=f'Default: {value}')
    for command in (setup, commands.add_parser('run'), commands.add_parser('uninstall')):
        command.add_argument('--prefix', default=str(DEFAULT_PREFIX), help='Dedicated install directory.')
    commands.add_parser('build-bundle', help='Build an optional local backend bundle; requires source build tools.').add_argument('--output', required=True)
    commands.choices['uninstall'].add_argument('--yes', action='store_true', help='Confirm deletion of all install data.')
    return root


def interrupted(_signum, _frame):
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    raise KeyboardInterrupt


def main():
    args = parser().parse_args()
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        {'install': install, 'run': run, 'uninstall': uninstall,
         'build-bundle': lambda options: build_dependencies(Path(options.output).resolve())}[args.command](args)
    except KeyboardInterrupt:
        print('Stopped.', file=sys.stderr)
        return 130
    except (SetupError, OSError, ValueError, tarfile.TarError, subprocess.TimeoutExpired) as error:
        print(f'Setup error: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
