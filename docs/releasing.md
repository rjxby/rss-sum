# Release process

The [release workflow](../.github/workflows/release.yml) runs when a `v*` tag is pushed. It builds RSS Sum with CGO enabled for SQLite and packages the executable with the installer, uninstaller, Python setup helper, README, license, example configuration, and setup guide. It checks the packaged scripts' syntax and help commands, then publishes the archives with `SHA256SUMS` and generated release notes after both platform builds pass. The tag is embedded as the application revision.

[CI](../.github/workflows/ci.yaml) owns tests, including installer tests through `make verify-fast`. CD does not build, download, or bundle gen-proxy, llama-runtime, or models. The installer handles those dependencies after explicit user consent. See [local setup](setup.md) for that contract and the [README](../README.md#releases) for supported platforms.

After merging the release commit into `main`, create and push a version tag:

```bash
git switch main
git pull --ff-only
git tag v0.1.0
git push origin v0.1.0
```

Replace `v0.1.0` with the version being released. Use a new version tag for each release. Tags containing a hyphen, such as `v0.2.0-rc.1`, publish as prereleases.
