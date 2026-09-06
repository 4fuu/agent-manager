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
- Both real microVM tests failed while downloading Alpine 3.22 from Docker Hub,
  before guest boot. WSL runtime operation is therefore not verified.
- Rootless Docker image build failed resolving `node:22-trixie`: DNS requests to
  `10.0.2.3:53` timed out. Direct Podman pull also timed out connecting to Docker
  Hub. No supplied Agent image build completed; network access must be repaired
  before retrying the commands in [images/README.md](../images/README.md).

## Current opt-in checks

The checks below are destructive only to uniquely named test guests, but download
images and require a working native runtime:

```powershell
$env:AGENT_MANAGER_LIVE_TEST = '1'
go test -v ./internal/backend -run TestMicrosandboxLiveDiskAndFileMount -count=1 -timeout 5m
go test -v ./internal/supervisor -run TestLiveWorkspaceTitlesDirectoryAndReconnect -count=1 -timeout 10m
Remove-Item Env:AGENT_MANAGER_LIVE_TEST
```

For manual UI verification, run `serve --fake` separately and exercise Project
creation, image selection, Session creation, failed setup logs/retry/recovery,
shell and Yazi columns, mouse hit routing, focus, scroll/reveal, width adjustment,
rename, resize, detach/reopen, stop/resume and deletion. Repeat the relevant path
with the real native runtime before recording it as runtime coverage.

## Unverified boundaries

- Building/importing the supplied images and registry login.
- Private Git clone and actual URI, Pi, OMP, Claude and Codex login/compatibility.
- Host-reboot recovery, Windows ARM64, Linux and macOS end-to-end operation.
- Explicit multi-client input/resize ownership and edit-conflict detection.
