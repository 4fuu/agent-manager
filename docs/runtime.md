# Runtime and compatibility

## Lifecycle

The TUI is a client of a same-user supervisor. All cloning, setup, shells, Yazi and
Agent commands execute in the Linux guest through `internal/backend`; failures do
not fall back to host execution or WSL.

Creating a Session snapshots its Project, image profile, environment and merged
shared/profile mappings. The supervisor persists its identity before creating or
reconnecting `am-<session-id>`. It clones the default branch through a staging
directory into `/workspace/repo`, runs `.agents/setup` there (shebang when
executable, Bash otherwise), and then attaches the initial Agent pane through
guest tmux. Missing setup is success; failed setup does not start the Agent.

Each additional shell or Yazi column has a durable pane ID and width and its own
guest tmux session inside the same Session microVM. Agent, shell, Yazi and recovery
panes share that VM's filesystem, processes and network; adding a column never
creates another sandbox. The supervisor preserves attachments across UI exits.
OSC titles affect only the leftmost pane's automatic Session title; a manual Name
takes precedence.

Session Stop cancels work and stops the VM while retaining its managed disk. Delete removes
the disk but retains host mappings and manager logs. On restart, known runtime
identities are inspected without booting; a missing known identity is reported and
never replaced automatically. Host reboot does not restore RAM processes.

The `agent-manager stop` command stops the supervisor instead:
it detaches guest clients and retains running guests. `uninstall` additionally
removes its login registration, not its state or disks. Service stop waits for
both IPC shutdown and the supervisor lifetime lock to be released before allowing
a subsequent start. The private IPC endpoint exposes health and shutdown using
the same current-user authentication as normal RPC.

Native registration must preserve detached guest processes. Linux units use
`KillMode=process`; macOS LaunchAgents use `AbandonProcessGroup`. On Windows,
microsandbox 0.6.17 [requests CREATE_BREAKAWAY_FROM_JOB for detached guests](https://github.com/superradcompany/microsandbox/blob/v0.6.17/sdk/rust/lib/runtime/spawn.rs#L547-L554),
which fails in Task Scheduler's restrictive job. The login task therefore uses
local WMI process creation with that flag to launch a same-user background worker
outside the task job, and waits for its exit. It transfers the login environment
in memory, not into task XML. Guest execution still goes through `internal/backend`;
WMI only starts the host supervisor. Do not replace this with direct task-child
execution or attached microVMs without rechecking native guest lifetimes.

Windows releases beginning with 2026.907.0 include a versioned GUI-subsystem
service host. Task Scheduler starts this host without a console. It runs the WMI
launcher with `CREATE_NO_WINDOW`; WMI starts a second GUI host outside the job,
which runs `service-run` with `CREATE_NO_WINDOW`. Service management commands also
use this flag for PowerShell and schtasks. Hiding or detaching an already-created
console is not equivalent: it can still flash or leave a Windows Terminal window.
The CLI remains a console application so its interactive TUI and `serve` work
normally. `install` validates the companion host before stopping an existing
registration; upgrades must rerun `install` to replace the old task action.

## Images

The supplied Debian Trixie recipe has local targets for URI Agent, Pi, OMP,
Claude Code and Codex. Their common base contains Git, GitHub CLI, ripgrep, fd,
Yazi, Bash, tmux, curl, Node.js, Python and build tools. No registry images are
bundled in the application archive; release images are downloaded from GHCR.
Agent Manager does not install an OCI builder.

Profiles accept an OCI reference or an absolute OCI archive path. Archives are
loaded automatically into microsandbox through `Image.Load`. Registry credentials
are separate from Git clone credentials. Build commands and exact targets are in
[images/README.md](../images/README.md).

Explicit image downloads belong to the supervisor and do not create VMs. Since
the pinned Go SDK lacks a standalone pull API, the backend streams verified OCI
blobs into a private temporary Docker archive and calls `Image.Load`. Progress
counts archive bytes, including metadata; import is a separate phase. Temporary
archives are removed after success, failure or cooperative cancellation. A hard
process kill can leave an OS temporary directory; transfers retry from the start,
not from a saved byte offset. A completed import remains in the runtime cache.

The guest runs as root and uses a managed 16 GiB disk, 4 GiB memory and 2 vCPUs.
Mappings are explicit bind mounts, read-only unless `rw` is selected. Files and
directories are accepted; guest workspace and manager-path collisions are not.
Nothing is implicitly mounted, and config hooks are not marked `Noexec`.

## State and ownership

Current state format is **version 3**. There is no migration from older versions;
use a new state directory. Unknown or malformed versions are rejected without
deleting them. State is atomically replaced and protected for the current user.
Runtime IDs, resolved configuration, pane IDs and widths are durable. Selection,
pane focus, reveal position and scroll are per-UI cache.

One supervisor is the authoritative owner for a state directory. Multiple clients
poll it, but revision-based conflict detection and explicit single-client terminal
input/resize ownership remain design gaps.

## Upstream and verification boundaries

The Go module pins microsandbox v0.6.17 and uses its detached sandbox, managed disk,
bind mount, TTY and `Image.Load` APIs. Native Windows x64/WHP is the tested runtime
baseline; the upstream Windows runtime remains preview. Linux x64/KVM is tested
under WSL2. Other Linux hosts and macOS require their own hardware verification.

The five Linux amd64 images build and pass archive import and guest startup checks.
Registry login, private clone, and authenticated Agent/model workflows have not
been verified. See [verification.md](verification.md).
