# Agent images

## GHCR downloads

Release images use `ghcr.io/4fuu/agent-manager/TARGET:VERSION`, where `TARGET` is
`uri`, `pi`, `omp`, `claude` or `codex`. Dated tags match the application version
(for example `2026.906.0`) and contain Linux amd64 and arm64 variants. `latest`
tracks the most recently published release; built-in profiles use dated tags.
The release's `images.json` records immutable multi-architecture digest references.

Press `i` in Agent Manager, select a profile and choose **Download**. This pulls
directly into microsandbox's cache and requires no Docker daemon. Custom profiles
can use other registries or a digest reference. Existing profiles retain their
configured references across application upgrades; edit them to select a newer image.

## Local builds

`Containerfile` defines five local build targets:

```sh
docker build -f images/Containerfile --target uri    -t uri-agent-manager:2026.904.3 .
docker build -f images/Containerfile --target pi     -t uri-agent-manager-pi:local .
docker build -f images/Containerfile --target omp    -t uri-agent-manager-omp:local .
docker build -f images/Containerfile --target claude -t uri-agent-manager-claude:local .
docker build -f images/Containerfile --target codex  -t uri-agent-manager-codex:local .
```

Agent Manager does not install Docker/Podman. The tags above are local build names;
`bash script/images.sh TARGET` builds and smoke-tests a GHCR-named image locally
without pushing it. Publication is owned by the [release workflow](../docs/release.md).
All five Linux amd64 targets were built with Docker in WSL2 and their exported
archives booted in both Windows WHP and WSL2 KVM on 2026-09-06. Python venv/pip,
Node execution, required tool availability and Agent version commands passed.
Agent authentication and model requests are not covered by these smoke tests.

## Included environment

- Python 3.13: `python`, `python3`, pip and venv. Use a virtual environment for
  project dependencies: `python -m venv .venv`, then `. .venv/bin/activate`.
- Node.js 22 and npm; Oh My Pi also includes Bun.
- Git, GitHub CLI, OpenSSH client, curl and CA certificates.
- ripgrep (`rg`), fd, fzf, jq, Yazi/ya, Bash, tmux, less and file.
- GCC/G++, make, pkg-config, Python venv tooling and OpenSSL development headers.
- zip/unzip, xz, process tools, iproute2, ping and DNS utilities.

Target Agent versions can be set with `URI_VERSION`, `PI_VERSION`, `OMP_VERSION`,
`CLAUDE_VERSION` and `CODEX_VERSION`. Defaults are pinned to the versions below;
the Debian/Node base and distribution package updates can still change on rebuild.

## Offline archives and size

Measured Linux amd64 builds (decimal MB/GB, rounded):

| Target | Agent version | Archive | Expanded filesystem |
| --- | --- | ---: | ---: |
| URI | 2026.904.3 | 560 MB | 1.56 GB |
| Pi | 0.73.1 | 521 MB | 1.63 GB |
| Oh My Pi | 18.1.11 | 800 MB | 2.58 GB |
| Claude Code | 2.1.263 | 586 MB | 1.63 GB |
| Codex | 0.153.4 | 619 MB | 1.75 GB |

Archive sizes are `docker save` file lengths with Docker 29's containerd image
store (compressed layer blobs); expanded sizes use `du -sx --block-size=1 /` in
a fresh container. Other Docker storage backends may export larger archives.
Five separate archives total about 3.09 GB and duplicate the common base; Docker
shares common layers locally. Expanded sizes exclude project dependencies,
runtime caches and the Session's managed disk allocation.

Export a single image per file, for example:

```sh
docker save -o uri.tar uri-agent-manager:2026.904.3
```

Set the image profile's archive field to the absolute host path of this `.tar`.
Session creation imports it through microsandbox `Image.Load`. Both Docker save
and OCI layout archives are accepted. Do not gzip the outer tar: runtime 0.6.17
requires an uncompressed archive container, although its layer blobs may be
compressed. Docker's local image store is separate from microsandbox's store;
a Docker tag alone does not make a locally built image available to microsandbox.

All profiles can receive explicit shared/profile file or directory mappings.
They are read-only by default; `rw` is opt-in. Nothing from the host is mounted
implicitly. Mapped credentials and executable config hooks are available to setup
and Agent processes.
