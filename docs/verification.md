# Behavior coverage

Default tests use a synthetic backend and never execute guest commands on the
host. Opt-in native tests use the application's microsandbox backend and boot a
real microVM. Fake results do not prove isolation, images or Agent compatibility.

## Recorded native Windows x64 baseline (2026-09-06)

Windows build 26100, amd64, Go 1.27.0 with CGO/GCC; WHP reported
`HypervisorPresent=true`. Tests ran natively, not through WSL.

| Check | Recorded result |
| --- | --- |
| `runtime-install`; `doctor` for microsandbox 0.6.17 | Passed |
| Alpine 3.22 WHP guest boot and guest shell | Passed |
| Single-file bind from a Windows path with spaces/Chinese; ro rejection and rw update | Passed |
| Real guest PTY, Chinese input/output and resize | Passed |
| Disk stop/start retaining marker and runtime ID | Passed |
| Native ConPTY UI lifecycle against fake backend | Passed |
| Protected state ACLs, same-user pipe identity and duplicate-supervisor locking | Passed |
| `go test ./...`, race tests, vet and CLI build at that baseline | Passed |

The redesigned workspace also passed these native Windows checks:

- Real WHP guest: mapped directory with a Chinese Windows path, setup reading
  mapped config and producing normal logs, writable host output, tmux OSC title,
  closing a sibling pane and reconnecting through a restarted supervisor. A second
  terminal reads a guest-local file written by the Agent terminal and compares
  the kernel boot ID, verifying both terminals run in the same sandbox.
- Actual ConPTY TUI against the fake backend: Project and image forms, Session
  creation, terminal input, multiple columns, rapid width adjustment, manual name,
  mouse menu, failed setup logs, recovery shell, whole-workspace switching and
  narrow-window resize. Rendered forms, columns, failure logs and narrow layout
  were inspected. Partial neighbour columns are intentional overflow previews.
- Full unit tests, race tests, vet and native CLI build.

## WSL2 Arch Linux x64 check (2026-09-06)

Go 1.26.2 with GCC, WSL kernel 6.6.87.2 and accessible `/dev/kvm`:

- Full unit tests, race tests, vet and Linux CLI build passed.
- Runtime 0.6.17 installed; `doctor` passed prerequisite and file checks.
- Both real microVM tests passed: file mounts, PTY, persistent disk, directory
  mappings, setup, same-sandbox sibling terminals, titles and reconnect.

## Agent image checks (2026-09-06)

All five Linux amd64 targets built with Docker in WSL2. Each exported `.tar` was
imported through the production backend and booted on Windows WHP and WSL2 KVM.
Every image passed Python venv creation, pip execution, Python ssl/sqlite imports,
Node execution, common tool lookup and its Agent's `--version` command. The build
also checks Agent availability in a login Bash shell. These are startup smoke
tests, not authenticated Agent/model workflows. Sizes and installed versions are
recorded in [images/README.md](../images/README.md).

## Image manager checks (2026-09-06)

- Native Windows and WSL2 both downloaded `ghcr.io/astral-sh/uv:0.8.0`
  by tag and digest through the production backend, imported it and inspected the
  cache by the original reference. These checks did not create a sandbox or use
  a Docker daemon. Private registry authentication remains unverified.
- Native Windows ConPTY against the fake backend exercised custom image creation,
  validation, editing, download/cancel/retry, UI detachment during a transfer,
  persisted completion after reopening, mouse download and visible failure.
  Rendered forms, progress, completion and failure states were inspected.
- Unit tests cover registry streaming, platform validation, cancellation,
  supervisor job persistence and recovery, source-edit invariants and UI focus.
  Full tests, race tests, vet and builds passed on Windows and WSL2.

## First release checks (2026-09-06)

[v2026.906.0](https://github.com/4fuu/agent-manager/releases/tag/v2026.906.0)
passed the [release workflow](https://github.com/4fuu/agent-manager/actions/runs/34024004986):

- Tests, race checks, vet, builds and installer fixtures on Windows amd64,
  Linux amd64/arm64 and macOS arm64.
- All five image builds and Agent/Python/Node/tool smoke tests on native amd64
  and arm64 Docker runners; anonymous GHCR manifest access for every target.
- Installation from the published program archives on all four platforms,
  including Windows Scoop and Apple Silicon Homebrew.
- The actual release Windows and Linux amd64 bundles installed locally and passed
  `--version` and `doctor` natively on Windows and in WSL2.
- The published URI image was pulled through the production downloader, imported
  and booted on Windows WHP and WSL2 KVM. URI, Python, Node, pip/venv and common
  tool checks passed in both guests; temporary test VMs were deleted afterward.

ARM64 image smoke tests run in containers, not microVMs. macOS and Linux ARM64
hardware guest boot and authenticated Agent workflows remain unverified.

## Current opt-in checks

The checks below are destructive only to uniquely named test guests, but download
images and require a working native runtime:

```powershell
$env:AGENT_MANAGER_LIVE_TEST = '1'
go test -v ./internal/backend -run TestMicrosandboxLiveDiskAndFileMount -count=1 -timeout 5m
go test -v ./internal/supervisor -run TestLiveWorkspaceTitlesDirectoryAndReconnect -count=1 -timeout 10m
Remove-Item Env:AGENT_MANAGER_LIVE_TEST
```

To verify a built image archive on either host, set `AGENT_MANAGER_LIVE_TEST=1`
and `AGENT_MANAGER_IMAGE_ARCHIVE` to its absolute `.tar` path, then run:

```sh
go test -v ./internal/backend -run TestMicrosandboxLiveAgentArchive -count=1 -timeout 7m
```

To exercise a real registry pull without creating a VM, set
`AGENT_MANAGER_LIVE_TEST=1` and
`AGENT_MANAGER_PULL_IMAGE=ghcr.io/astral-sh/uv:0.8.0`, then run:

```sh
go test -v ./internal/backend -run TestLiveRegistryDownloadWithoutSandbox -count=1 -timeout 7m
```

For manual UI verification, run `serve --fake` separately and exercise Project
creation, image selection, Session creation, failed setup logs/retry/recovery,
shell and Yazi columns, mouse hit routing, focus, scroll/reveal, width adjustment,
rename, resize, detach/reopen, stop/resume and deletion. Repeat the relevant path
with the real native runtime before recording it as runtime coverage.

## Unverified boundaries

- Private registry login and ARM64 microVM execution.
- Private Git clone and actual URI, Pi, OMP, Claude and Codex login/compatibility.
- Host-reboot recovery, Windows ARM64, Linux and macOS end-to-end operation.
- Explicit multi-client input/resize ownership and edit-conflict detection.
