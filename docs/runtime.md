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

Stop cancels work and stops the VM while retaining its managed disk. Delete removes
the disk but retains host mappings and manager logs. On restart, known runtime
identities are inspected without booting; a missing known identity is reported and
never replaced automatically. Host reboot does not restore RAM processes.

## Images

The supplied Debian Trixie recipe has local targets for URI Agent, Pi, OMP,
Claude Code and Codex. Their common base contains Git, GitHub CLI, ripgrep, fd,
Yazi, Bash, tmux, curl, Node.js, Python and build tools. No registry images are
published and no OCI builder is installed by Agent Manager.

Profiles accept an OCI reference or an absolute OCI archive path. Archives are
loaded automatically into microsandbox through `Image.Load`. Registry credentials
are separate from Git clone credentials. Build commands and exact targets are in
[images/README.md](../images/README.md).

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
baseline; the upstream Windows runtime remains preview. Linux and macOS remain
platform goals and require their own hardware verification.

Image build/import, registry login, private clone, and real URI/Pi/OMP/Claude/Codex
login and compatibility have not been verified. See [verification.md](verification.md).
