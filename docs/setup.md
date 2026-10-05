# Local setup

The release installer provisions RSS Sum, gen-proxy, and two llama-runtime gRPC backends in a dedicated directory. The default is `~/.local/share/rss-sum`. Summaries run locally. Release archives contain RSS Sum and its setup scripts. The installer downloads backends and a model only after you agree. The running app needs network access to fetch public feeds.

## Requirements

- Linux AMD64 with glibc 2.35 or newer, or macOS ARM64 with macOS 14 or newer.
- Python 3.9 or newer and OpenSSL on PATH.
- At least 4 GB of available memory and 2 GB of free disk space for the default model and stack. Larger models need more memory and storage.
- On macOS, an unlocked login keychain for the private localhost certificate.

On macOS ARM64, setup downloads self-contained [gen-proxy v0.5.0](https://github.com/rjxby/gen-proxy/releases/tag/v0.5.0) and [llama-runtime v0.7.1](https://github.com/rjxby/llama-runtime/releases/tag/v0.7.1) binaries. It verifies the SHA-256 values pinned in `scripts/setup/local_stack.py` before using them. You do not need a .NET SDK, Go installation, or compiler for release installation on this platform.

These upstream releases do not provide Linux binaries. On Linux, supply `--stack-bundle` or choose `--build-backends`. Building backends requires .NET SDK 10, a C/C++ compiler, CMake, make, unzip, and shasum. It downloads pinned source revisions and build dependencies; the runtime's build verifies its pinned llama.cpp downloads. Installation from an RSS Sum source checkout also needs the Go version in `go.mod` and a CGO-capable C compiler to build the app.

## Install and run

Extract a release archive, then run:

```bash
./install.sh
```

The terminal wizard offers defaults for ports, the Hugging Face model URL and SHA-256, feeds, polling interval, item limit, and summary timeout. It uses text prompts and needs no TUI package. After configuration, it lists the downloads and asks `Download and install these dependencies? [y/N]`. Only `y` or `yes` approves setup. Enter, any other answer, or end of input cancels before setup creates files or makes downloads.

`--defaults` skips configuration prompts but retains the consent prompt. Without a terminal, setup requires `--accept-downloads`. For unattended setup on macOS ARM64:

```bash
./install.sh --defaults --accept-downloads
~/.local/share/rss-sum/run.sh
```

For Linux source builds, add `--build-backends` to the install command. Consent covers backend and model downloads, local configuration, and certificate setup. Setup validates configuration, refuses occupied ports, copies RSS Sum, obtains the backends, downloads and verifies the model, creates separate random public and runtime API keys, prepares TLS, and checks both backends and the proxy. It stops the services after the check. Run the generated launcher to start the app. Open [RSS Sum](http://127.0.0.1:8080).

The default model is [bartowski's Llama 3.2 1B Instruct Q4_K_M GGUF](https://huggingface.co/bartowski/Llama-3.2-1B-Instruct-GGUF). Its URL includes an immutable repository revision and setup checks its SHA-256. Review the model repository's license before using it. Both gRPC backends load the same downloaded file, each with one inference worker and a 512-token generation budget. Generation uses a 4096-token context; prompt reduction uses 8192 tokens so it can shorten inputs that exceed the generation budget. Inputs that exceed the reducer's budget can still fail. Adjust `--generation-context` and `--summarizer-context` for another model. Model quality is not measured by the deterministic installer tests.

The launcher starts these services in order:

| Service | Default endpoint | Purpose |
| --- | --- | --- |
| Generation llama-runtime | `https://localhost:50051` | Token estimation, capabilities, structured generation through gRPC. |
| Prompt-reduction llama-runtime | `https://localhost:50052` | Shortens inputs that exceed the generation context. |
| Gen-proxy | `http://127.0.0.1:7001` | Responses API gateway to the two backends. |
| RSS Sum | `http://127.0.0.1:8080` | Feed processing, SQLite storage, and web UI. |

This follows the [gen-proxy stack example](https://github.com/rjxby/gen-proxy#local-development). Gen-proxy uses `GenerationRuntime__Address`, `PromptReducerRuntime__Address`, and their outbound API keys to connect to the gRPC servers. Each server uses `HostedModel__ModelPath` to select the GGUF file. `GEN_PROXY_MODEL` names the model in RSS Sum's request; it does not select the file loaded by a backend.

Ctrl+C or SIGTERM stops the app, proxy, and both backends. If any backend or the proxy exits unexpectedly, the launcher stops the remaining processes and fails. Logs for the proxy and backends are under `logs/`; RSS Sum writes to the launching terminal. The launcher runs in the foreground and does not install an OS service or start at login.

## Customize

Command-line settings override wizard defaults. `--defaults` also accepts overrides:

```bash
./install.sh --defaults \
  --prefix "$HOME/.local/share/rss-sum-work" \
  --http-port 8090 --proxy-port 7002 \
  --generation-port 50061 --summarizer-port 50062 \
  --feeds 'https://go.dev/blog/feed.atom,https://hnrss.org/frontpage' \
  --interval 1800 --item-limit 2 --timeout 300
```

For another public GGUF model, set `--model-url`, `--model-sha256`, and `--model-id` together. Use a Hugging Face `https://huggingface.co/<owner>/<repo>/resolve/<commit>/<file>.gguf` URL and the file's LFS SHA-256 from that revision. Setup supports direct public GGUF downloads. Gated model authentication and conversion from other formats are not included.

`./install.sh --help` lists all options. `--binary /path/to/rss-sum` selects an existing app executable. `--stack-bundle /path/to/local-stack` copies a prebuilt dependency directory with matching `versions.json` instead of downloading or building backends. The model still downloads after consent. A missing bundle fails setup without switching to a source build. `--build-backends` explicitly chooses source builds. It cannot be combined with `--stack-bundle`.

To prepare an optional backend bundle locally with the source build prerequisites:

```bash
python3 scripts/setup/local_stack.py build-bundle --output /tmp/rss-sum-local-stack
./install.sh --stack-bundle /tmp/rss-sum-local-stack
```

Installed `config.json` contains the settings shown in the wizard. Edit feeds, ports, interval, item limit, or timeout, or context sizes while the launcher is stopped. Changing the model URL or hash in that file does not download a new model. To change the model, install into a new prefix or uninstall and reinstall. `keys.json` contains the API keys and certificate password. The install directory is private to the installing user. The launcher supplies the app's environment settings from these files, overriding inherited provider and database settings. It does not load the source checkout's `.env`.

Existing install directories are never overwritten. Separate installations need separate prefixes and distinct ports. A lock prevents a second launcher, setup, or uninstall from modifying an active installation. The Go worker still enforces public feed destinations, including DNS, redirects, and connection checks.

## Certificates

The gRPC backends require HTTPS. Setup creates a private self-signed server certificate restricted to localhost and stores it under `tls/`. On Linux, the proxy uses the private certificate file through `SSL_CERT_FILE`; setup does not change the system trust store. On macOS, setup adds the certificate to the user's login keychain with SSL trust restricted to `localhost`. It may display the macOS keychain authorization dialog. Uninstall removes that trust and the certificate by fingerprint before deleting its files.

The public proxy API uses HTTP bound to `127.0.0.1`. The gRPC endpoints bind to localhost. Do not expose these local stack endpoints to the network.

## Uninstall and failed setup

Stop the launcher first, then run either the release script or the installed script:

```bash
./uninstall.sh --yes
# Or:
~/.local/share/rss-sum/uninstall.sh --yes
```

For a custom directory:

```bash
./uninstall.sh --prefix "$HOME/.local/share/rss-sum-work" --yes
```

Without `--yes`, uninstall requires typing `remove` in a terminal. It deletes the owned install directory, including the database, downloaded model, executables, extracted .NET cache, logs, keys, and TLS files. It removes the macOS certificate trust first. It leaves the downloaded release archive and source checkout in place.

Setup failures preserve an incomplete installation and print its cleanup command. Uninstall accepts that incomplete marker. It refuses directories without a matching ownership marker, symlinked prefixes, and active installations. If keychain cleanup fails, it keeps the files so cleanup can be retried. Running uninstall again after successful removal reports that nothing is installed.
