# Using Agent Manager

## Projects and Sessions

A Project contains a name and credential-free `https://github.com/owner/repo` URL.
Saving it does not create a guest. **New Session** selects a Project and image
profile, snapshots that configuration, clones the repository's default branch,
runs `.agents/setup`, then starts the profile command. Later profile or shared
mapping edits do not alter existing Sessions.

The left tree is Project → Session. The selected Session owns a horizontal set of
terminal columns. Pane IDs and preferred widths are durable; active pane, reveal
position and scroll/focus state are local to each UI. The leftmost pane's OSC title
may name an unnamed Session. A manually assigned **Name** remains fixed.

## Controls

| Key | Action |
| --- | --- |
| `p` / `n` | New Project / Session |
| `w` | Project / Session picker, including when the sidebar is hidden |
| `i` / `g` | Image profiles / shared mappings |
| `Space` | Context menu |
| `Tab` | Focus the workspace; `Ctrl+]` returns to manager navigation |
| Left / Right | Move between terminal columns |
| `+` / `-` | Increase / decrease the active column width |
| `t` / `y` | Add a guest shell / Yazi column |
| `r` | Give the Session a fixed name |
| `Ctrl+Q` | Quit the UI from manager focus |

Mouse selection, forms, menus and pane hit routing are supported. Guest-requested
mouse events use pane-local coordinates. Use guest tmux copy mode (`Ctrl+B`, then
`[`) for terminal scrollback. Setup logs support arrows and wheel scrolling; `l`
switches between logs and workspace. Terminal output itself is not persisted.

The Space menu also provides start/reconnect, stop, delete, setup retry, recovery
shell, close-column and Project editing actions as applicable. Delete requires
confirmation. Stop preserves the disk.

## Image profiles and mappings

Built-in profiles are URI, Pi, OMP, Claude Code and Codex. A custom profile accepts
exactly one OCI reference or absolute OCI archive path, a command, environment
JSON and mappings. Archive selection is imported automatically with the SDK's
`Image.Load`; see [image setup](../images/README.md).

Shared and profile mappings use one entry per line:

```text
C:\Users\alice\Agent config | /root/.config/uri-agent | ro
C:\Users\alice\AppData\Roaming\GitHub CLI | /root/.config/gh | ro
C:\Users\alice\.codex\auth.json | /root/.codex/auth.json | rw
```

The form is `absolute host path | absolute guest path | ro or rw`; `ro` is the
default. Existing regular files and directories are accepted. No host path is
mounted implicitly. Guest `/workspace` and `/run/manager` collisions, overlapping
guest targets, devices and non-absolute paths are rejected. There is no special
ban on a home-root source and mapped config hooks are executable according to
normal guest filesystem behavior.

Shared entries are merged by guest path, with profile entries taking precedence
at the same path. Put common tools such as `gh` in shared mappings and the Agent's
configuration in its image profile. The supplied images configure Git to use
`gh auth git-credential` for GitHub, so a mapped gh configuration can authenticate
clone as well as later guest Git operations. Host OS credential-vault references
are not portable credentials; use a config containing a usable token or the
dedicated token-file option below. Prefer a writable directory when an Agent
refreshes credentials using atomic file replacement.

Projects are trusted. Mapping credentials intentionally makes them available to
repository setup and Agent code. Directory mappings have no credential scanning
or suppression: their logs run normally and can contain emitted secrets. Setup
log redaction recognizes complete lines and JSON string values from individual
mapped files and the dedicated Git token file. It cannot recognize transformed,
generated, encoded or recursively discovered values. Treat logs accordingly.

## Private GitHub repositories

The Project's GitHub token field accepts an absolute path to a dedicated regular
file, not a token value. The file must be private to the current user and is made
available read-only at `/run/manager/github-token`; the supplied askpass helper
uses it for clone. Repository URLs, image references and commands should not embed
credentials. No host Git configuration, home directory or SSH agent is mounted
implicitly.

## Supervisor and state

The UI uses a private Unix socket or same-user Windows named pipe. Exactly one
supervisor owns a state directory. Multiple UIs can observe it, but explicit
multi-client terminal ownership and edit-conflict detection are not implemented;
avoid typing into one Session from multiple clients.

Defaults are `%LOCALAPPDATA%\agent-manager` on Windows and
`$XDG_STATE_HOME/agent-manager` (or `~/.local/state/agent-manager`) on Unix. UI and
supervisor must use the same `--state`; microsandbox data remains under `$MSB_HOME`
or its default. Do not hand-edit live state.

## Windows

Windows support is native and requires WHP; WSL is not a fallback. If `doctor`
reports no hypervisor, enable WHP from elevated PowerShell and reboot:

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName HypervisorPlatform -All -NoRestart
```

Run Agent Manager as the normal user. `runtime-install` installs the pinned runtime
under `%USERPROFILE%\.microsandbox` (or `%MSB_HOME%`). Protected ACLs are applied
to manager state. Credential files outside it are checked, not re-permissioned.
