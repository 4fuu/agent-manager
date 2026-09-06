# Dated releases

## Version source

`internal/buildinfo/version.txt` is the single application and default-image
version source. Versions follow URI Agent's `YYYY.MDD.REVISION` convention in
Asia/Hong_Kong: the month is unpadded, the day is two digits and the first release
of a day uses revision zero. Tags add `v`, for example `v2026.906.0`.

```sh
python script/release.py version --set 2026.906.0 --today
```

Choose the next unused revision for that date. Published application releases are
immutable; use a new revision for changed binaries or images. Existing Sessions
and user-edited profiles are never rewritten by the version command.

## Prepare and verify

1. Update the version and any intended pinned Agent versions in
   `images/Containerfile`.
2. Run the [repository checks](../AGENTS.md#verification), plus
   `python -m unittest discover -s script -p 'test_*.py' -v`.
3. Build a native program archive with `python script/release.py build` and an
   image with `bash script/images.sh uri`. Local image builds need Docker;
   application users do not.
4. Review the diff, commit and push to `main`.
5. Dispatch the **Release** workflow on `main`:

```sh
gh workflow run release.yml --ref main
```

The workflow validates the calendar date and existing tag, runs CI, builds native
program bundles, and builds/tests all five images on both amd64 and arm64 runners.
It uses the repository `GITHUB_TOKEN`, with package/content write permission only
in publishing jobs. No long-lived registry password is needed. Releases are
serialized; a second dispatch does not cancel an active release.

## Artifacts and publication

Program bundles are Windows amd64 ZIP, Linux amd64/arm64 tar.gz, and macOS arm64
tar.gz. Linux builds use Debian 12 for the glibc baseline. CGO is enabled; the SDK
embeds its native FFI, so no adjacent FFI DLL/SO is required. `runtime-install`
downloads the separate VM executable and firmware.

Images are published under `ghcr.io/4fuu/agent-manager/{uri,pi,omp,claude,codex}`.
Each target has a dated multi-architecture tag, native architecture tags, and a
`latest` alias. Built-in profiles use the dated tag, not `latest`.

After all image smoke tests and binary builds pass, the workflow:

1. Combines architecture images into dated manifests and records their digests.
2. Requires anonymous access to every GHCR image.
3. Produces `SHA256SUMS`, generates Scoop/Homebrew metadata with exact archive
   hashes, and commits metadata to `main`. Unrelated movement of `main` aborts
   publication; metadata-only movement permits retry of the same workflow.
4. Creates the release tag at the verified source commit, uploads the four program
   archives, `SHA256SUMS` and `images.json`, then publishes the release and updates
   image `latest` aliases. Image tar files are never uploaded to Releases.
5. Installs the published packages on all four build targets, including native
   Windows standalone/Scoop and Apple Silicon Homebrew checks.

## First GHCR publication

GitHub creates personal container packages as **private**, even when their linked
repository is public. Repository permission inheritance does not change package
visibility. After the first push, open each package's settings under
[account packages](https://github.com/4fuu?tab=packages), choose **Change visibility**
and make it **Public**. This is a one-time setting per package; GitHub does not
offer a documented REST endpoint for changing it. Making a package public is
irreversible.

The release job refuses to publish an application whose default images cannot be
pulled anonymously. If it stops at this check, change all five packages to Public
and rerun the failed jobs. Keep the verified source unchanged while retrying.

## Failure and retry

Build or image smoke failures stop release publication, although successfully
pushed architecture tags may already exist. Fix source failures under an unused
version and dispatch again. For external failures, rerun failed jobs of the same
workflow. Do not move an existing release tag or rebuild published dated images.
The upload step can resume an incomplete draft using that run's existing artifacts.

Installer smoke failure after publication is visible as a failed workflow; it does
not roll back a public release. Inspect the failing platform and publish a corrected
dated revision when code or artifacts must change. CI and installation checks do
not prove guest isolation, hardware boot or authenticated Agent compatibility;
record those separately in [verification](verification.md).
