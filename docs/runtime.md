# Runtime and compatibility

## Build the image

The [Containerfile](../images/Containerfile) builds Ubuntu 24.04 with URI Agent
2026.904.3, bundled retrieval assets, Git, Bash, tmux, flock, C/C++ build tools,
Python, Node/npm, ripgrep and jq. The pinned upstream installer verifies release
SHA256SUMS. Project-specific language toolchains belong in the guest's
`.agents/setup`; the manager never runs that script on the host.

Use Podman or another OCI image builder. Building an image is distinct from the
microsandbox execution backend; no container engine runs agent instances.

```sh
podman build -f images/Containerfile -t uri-agent-manager:2026.904.3 .
podman save --format oci-archive -o /tmp/uri-agent-manager.oci.tar \
  uri-agent-manager:2026.904.3
./agent-manager image-load --archive /tmp/uri-agent-manager.oci.tar
```

The import uses the official `microsandbox.Image.Load` API. Direct tar paths are
not OCI image references; choose the imported tag in the Project. A registry tag
or digest is also accepted. Registry credentials are not exposed by this manager;
import private images locally. The Project's GitHub token is for Git cloning, not pulls.
Image build and import have not been exercised in this orb (no running OCI builder).

The default image runs as guest root. MicroVM isolation, not Unix guest users,
is the host boundary. Each instance receives a managed 16 GiB persistent root
disk, 4 GiB guest memory and 2 vCPUs. Config files use `Mount.Bind` with `Readonly`
unless explicitly writable, plus `Noexec`, `Nosuid` and `Nodev`.

## Execution and recovery

1. Persist an instance ID and immutable Project snapshot.
2. Create a detached, non-ephemeral microsandbox named `am-<instance-id>`.
   Persist its runtime ID before cloning. If the manager loses the response,
   reconnect by the persisted unique name rather than creating a second identity.
3. Clone inside the guest into `/workspace/repo.init`; fetch the requested ref
   and atomically rename to `/workspace/repo`. Interrupted clones are retried from
   staging. No host workspace is mounted.
4. Run `.agents/setup` at `/workspace/repo`. Executable scripts use their shebang;
   non-executable scripts run through Bash. Absence is success. Stream redacted logs.
5. Persist setup completion both in manager metadata and a guest marker outside
   the checkout. A guest flock prevents concurrent setup after a lost supervisor.
6. Open a TTY `ExecStream` attaching to **guest tmux** session `agent`, which owns
   URI Agent. The manager retains the handle and terminal emulator across UI exits.

Failure does not launch the agent. Shell opens a separate guest tmux `recovery`
session (at `/` if clone did not complete). Retry explicitly clears the setup
completion marker after acquiring its lock. Returning from recovery with Start
finishes missing setup before attaching the agent. Stop cancels active work,
stops the microVM and retains its disk. It is not a suspend-to-RAM operation.
Resume boots the same disk and starts/attaches guest tmux without rerunning
completed setup. URI Agent's saved sessions remain available through its own UI
or a Project command such as `uri-agent --continue-session`.

On supervisor startup, runtime metadata is inspected without booting VMs. Running
VMs appear disconnected until Start reconnects; stopped VMs remain stopped;
missing known identities are never automatically replaced. Reconnecting a live
VM opens a fresh SDK PTY attachment to the same guest tmux session. The SDK cannot
reconstruct an old `ExecHandle`, so tmux supplies that guest-side continuity.
Graceful supervisor shutdown detaches the VM handle (not SDK `Close`, which can
stop detached guests). A host reboot loses live execution; persistent disks and
saved agent sessions can be resumed, but processes are not restored from RAM.

## Inspected upstream contracts

- microsandbox Go SDK **v0.6.17**, release
  [5dd55ebb](https://github.com/superradcompany/microsandbox/commit/5dd55ebb920f2690204390f86698d20f570d4dfa):
  `CreateSandbox`, `GetSandbox`, `StartDetached`, `Connect`, `Stop`, `Destroy`,
  `Detach`; `ExecStream` / `TakeStdin` / `Resize(rows, cols)`; `RootDisk.Managed`;
  `Mount.Bind`; `Image.Load`. Go modules pin the published version and FFI bundle.
- Its [`sandbox.go`](https://github.com/superradcompany/microsandbox/blob/v0.6.17/sdk/go/sandbox.go)
  explicitly distinguishes stopping `Close` from non-stopping `Detach`.
- Regular-file mounts use the runtime's `SingleFileFs`, exposing a single file
  rather than its parent directory. This was added before v0.6.17.
- The SDK includes a native FFI shim, not all runtime artifacts.
  `runtime-install` calls `EnsureInstalled` for pinned msb/libkrunfw components.
- URI Agent [bcdbf2f](https://github.com/4fuu/uri-agent/commit/bcdbf2f5183ee6f3d924131db6443631dc7d471c):
  real interactive CLI is `uri-agent`, with `--cwd`, `--continue-session` and
  `--session`; configuration resolves through `URI_AGENT_CONFIG_DIR`, then XDG
  config paths. The image preserves the complete upstream release asset directory.

Reference designs informed interaction and ownership, not a source port:

| Reference | Behavior used here | Difference |
| --- | --- | --- |
| [YoanWai/agent-manager](https://github.com/YoanWai/agent-manager), 42c8c196 | Outer manager owns chrome; nested terminal gets local mouse coordinates | tview + Charm VT and guest tmux, not host CLI/tmux control |
| [herdrdev/herdr](https://github.com/herdrdev/herdr), cc88b3b8 | Mouse-first multiplexer; terminal owned outside the client UI | Go supervisor with persistent runtime identity, not local PID lifetime |

## Unverified boundaries

No live KVM device is available in the development orb. Go compilation proves API
compatibility; fake tests prove the manager's state machine and TUI routing only.
Before relying on this for real work, run the supplied image on a KVM host and
verify private clone, read-only and writable single-file mounts, setup failure and
retry, real URI Agent input/resize, UI restart, supervisor restart, stop/resume and
delete. OCI build, firmware availability and microVM boot failures must be resolved
there. The implementation never substitutes host execution for a runtime failure.
