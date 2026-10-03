# Release process

The [release workflow](../.github/workflows/release.yml) runs when a `v*` tag is pushed. It runs tests with race detection on each target, builds with CGO enabled for SQLite, and publishes the archives with `SHA256SUMS` and generated release notes after all builds pass. The tag is embedded as the application revision. See the [README](../README.md#releases) for supported platforms and archive contents.

After merging the release commit into `main`, create and push a version tag:

```bash
git switch main
git pull --ff-only
git tag v0.1.0
git push origin v0.1.0
```

Replace `v0.1.0` with the version being released. Use a new version tag for each release. Tags containing a hyphen, such as `v0.2.0-rc.1`, publish as prereleases.
