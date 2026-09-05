# Using Agent Manager

## Manager and terminal controls

All primary actions have clickable buttons and keyboard shortcuts:

| Key | Action |
| --- | --- |
| F1 / F2 | Add / edit the selected Project |
| F3 | New instance; choose ref and image before launch |
| F4 | Start / resume / reconnect selected instance |
| F5 / F6 | Focus terminal / detach terminal focus |
| F7 | Stop VM, preserving its disk; cancels active setup first |
| F8 | Retry setup explicitly, then launch URI Agent |
| F9 | Open a guest recovery shell without marking setup complete |
| F10 | Delete instance, or selected Project when no instance is selected; confirmation required |
| F11 | Edit global config-file mappings |
| F12 | Toggle setup logs and terminal |
| Tab | Switch tree and right pane; advance fields inside a form |
| Esc | Cancel a form or confirmation |
| Ctrl+] | Return from the nested terminal to manager controls |
| Ctrl+Q | Quit UI while in manager mode |

Arrow keys select Projects/instances and scroll logs. Click a tree row to select
it. Click the terminal or Attach to focus it. While terminal-focused, keys,
function keys and bracketed paste belong to URI Agent; only Ctrl+] is reserved.
Clicking manager buttons remains possible. Guest mouse events are translated to
pane-local coordinates and emitted according to the guest's requested modes.
Terminal rendering and keyboard/mouse protocol encoding use Charm's VT library;
the manager does not execute or reinterpret the agent's commands.

Setup logs are plain text, selectable through your terminal, and scrollable using
the keyboard or wheel. The UI previews the latest 128 KiB; full redacted logs are
private files named `<instance-id>.log` in the state directory. Agent terminal
output is held in memory, not written to those logs. Use guest tmux copy mode
(Ctrl+B then `[`) for terminal scrollback.

## Projects and config mappings

A Project stores a name, credential-free HTTPS GitHub URL, image reference,
default branch/ref, guest shell command, optional GitHub token-file path, and
per-project mappings. Defaults are `main`, `uri-agent-manager:2026.904.3` and
`uri-agent`. Arbitrary Git refs are fetched and checked out detached; URI Agent
can create a working branch in its independent workspace. Empty ref uses the
repository's default branch.

One mapping per line in either the Project or global mapping form:

```text
/home/alice/config/settings.json | /root/.config/uri-agent/settings.json | ro
/home/alice/config/models.json | /root/.config/uri-agent/models.json | ro
```

The final field defaults to `ro`; `rw` must be explicit. Only existing individual
regular files are accepted. Directories, devices and guest workspace/manager-state
targets are rejected. Paths are absolute; `~` is not expanded. The `|` character
is the form delimiter and cannot be represented in its paths.

Global defaults are merged by guest path, with Project entries taking precedence.
An instance snapshots the merged result, image, ref and command when created.
Editing a Project or its defaults affects only new instances. This prevents a
running instance from changing its mounts behind the agent's back.

URI Agent uses `/root/.config/uri-agent` in the supplied image (`URI_AGENT_CONFIG_DIR`).
Its `settings.json`, `auth.json` and `models.json` are config files; its other
session state remains on the instance's private guest disk. Do not map the whole
config directory or entire home. Read-only `auth.json` works for pre-provisioned
credentials but prevents credential refresh/writes. A writable bind file may not
support applications that replace files by atomic rename; authenticate inside
the guest instead when that behavior is required.

## Private GitHub repositories

Create a dedicated fine-grained GitHub token with read access to only the target
repository. Write it into an absolute-path host file using your secret manager
or editor, then set mode 0600. Put **the file path**, not its contents, in the
Project's **GitHub token file** field.

The file is read-only mounted at `/run/manager/github-token`. The supplied image's
`manager-git-askpass` helper answers Git's credential prompts over a pipe. Clone
URLs, command arguments, Project JSON and manager logs never contain the token.
No host Git credentials or home directory are mounted implicitly. Git does not
persist this token into the cloned repository config. The token file remains
available in the guest for later explicit use; for Git commands after cloning,
set `GIT_ASKPASS=/usr/local/bin/manager-git-askpass GIT_TERMINAL_PROMPT=0` in the
guest. SSH agent forwarding is not implemented.

Mappings and the token file grant guest code access to their contents. Treat the
repository's setup script and URI Agent tools as having that authority. Use
read-only, short-lived, repository-scoped credentials. The manager redacts known
mapped JSON string values, config lines and the Git token before persisting setup
logs, including values split across stream chunks. Redaction is not a sandbox
against malicious scripts: transformed, encoded or newly generated secrets cannot
be reliably recognized. Do not print secrets in setup scripts. Never put secrets
in the launch command, image reference or repository URL.

## Supervisor service

The UI connects over a 0600 Unix socket inside a 0700 state directory. Only one
supervisor may own that directory; an advisory lock protects startup and stale
socket removal. Multiple UIs can observe the same instance. They share one PTY;
the last resize wins, so use a single terminal-focused client when typing.

For Linux desktop use:

```sh
install -Dm755 agent-manager "$HOME/.local/bin/agent-manager"
install -Dm644 contrib/agent-manager.service \
  "$HOME/.config/systemd/user/agent-manager.service"
systemctl --user daemon-reload
systemctl --user enable --now agent-manager
agent-manager
```

The supplied service deliberately uses `KillMode=process` so restarting the
supervisor does not kill detached microVM processes in its cgroup. Stop instances
explicitly in the UI before intentionally terminating all work. Host logout may
stop user services unless your system administrator enables lingering.

The default manager directory is `$XDG_STATE_HOME/agent-manager`, otherwise
`~/.local/state/agent-manager`. Use the same `--state DIR` for UI and supervisor.
Microsandbox's independent data directory is `$MSB_HOME`, otherwise
`~/.microsandbox`; both must remain available for recovery. Back up both while
instances are stopped. Do not hand-edit state while the supervisor is running.
