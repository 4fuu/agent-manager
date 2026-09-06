# Agent Manager: goals and design

This document separates current contracts from product goals. Operational details
belong in [usage](docs/usage.md), runtime facts in [runtime](docs/runtime.md), and
evidence in [verification](docs/verification.md).

## Product model

- A **Project** is a trusted GitHub repository definition. Creating one creates no
  guest and stores no branch/ref or Agent choice.
- A **Session** selects a Project and image profile, snapshots that configuration,
  clones the repository default branch, runs `.agents/setup`, and starts the Agent.
- Every Session has an independent durable disk and a niri-like horizontal set of
  terminal columns. Pane identity and width are durable; focus, reveal and scroll
  are local UI state.
- The supervisor, not a TUI, owns state, guests and terminal attachments. UI exit
  detaches; Stop retains disk; Delete removes disk.

All repository commands, setup, Agents, shells and Yazi run in the guest through
`internal/backend`. There is no host-execution or WSL fallback. A known missing
runtime is never silently recreated. State is versioned and atomically replaced;
version 3 has no migration path from older formats.

## Interaction

The primary hierarchy is Project → Session, with restrained textual status and a
contextual footer rather than an F1–F12 command bar. `Ctrl+]` transfers terminal
focus to manager navigation. `t` and `y` add shell and Yazi columns, arrows move
between columns, `+`/`-` change width, `r` fixes a Session name, `Space` opens its
menu, `i` manages image profiles and `g` manages shared mappings.

Only the leftmost pane contributes an OSC-derived automatic Session title. Manual
Name is stable. Mouse hit-testing and forwarded guest events use the same pane
geometry as rendering. Narrow windows reveal the active horizontal column rather
than compressing all terminals below useful width. Default cells preserve the
host terminal background while explicit guest colors remain intact.

Setup progress and failure logs remain available beside recovery actions. Terminal
contents are not manager logs. Forms and all primary actions must remain usable by
keyboard and mouse across focus changes and resize.

## Images, mappings and trust

Image profiles own Agent command, environment and mappings. Built-ins correspond
to local URI, Pi, OMP, Claude and Codex `Containerfile` targets; custom profiles
accept an OCI reference or archive path, with archives loaded via SDK `Image.Load`.
There are no claimed published images or bundled OCI builder.

Mappings are explicit files or directories, read-only by default. Nothing is
implicitly mounted. Guest targets may not collide with `/workspace` or manager
paths, and overlapping targets are rejected. Home-directory sources are permitted;
mapped config hooks retain normal executable behavior. Trusted setup and Agent code
can read mapped credentials. Known values from individual files are redacted from
setup logs, but directories are not recursively scanned and emitted credentials
may remain in logs.

## Architecture and platform goals

```text
Go TUI clients → private same-user IPC → supervisor → internal/backend
                                                   → native microVM → Linux guest
```

Native Windows is mandatory, with WHP as the tested x64 baseline; WSL cannot stand
in for Windows support. Linux/KVM and macOS Apple Silicon remain platform goals,
not claims established by the Windows result. CGO/FFI packaging, runtime install,
path semantics, permissions, terminal behavior and image architecture require
platform-specific verification.

The image/Agent matrix must separately verify build/import, public and private
clone, setup, login, input/mouse/resize, detach/reconnect, stop/resume and deletion.
Build success or fake coverage is not end-to-end Agent support.

## Reference direction

These repositories are interaction inspiration, not source ports:

- `YoanWai/agent-manager`: `internal/ui/view.go`, `styles.go` and `split.go` inform
  Project hierarchy, restrained statuses and contextual footer behavior.
- `herdrdev/herdr`: `src/client/shell/sidebar.rs`, `tabs.rs`, `mouse.rs`,
  `render.rs` and `theme.rs` inform default-background rendering, tabs/columns,
  hit routing and active-pane scroll reveal.

Agent Manager keeps its Go/tview/Charm VT implementation, guest tmux continuity
and persistent microVM ownership rather than porting either architecture.

## Remaining design gaps

- Explicit single-client terminal input/resize ownership with visible takeover.
- Revisioned multi-client updates and conflict detection for concurrent edits and
  lifecycle operations; the supervisor remains authoritative, but polling alone
  does not prevent stale writes.
- Complete responsive/form accessibility, terminal selection and abnormal-exit
  restoration verification in the actual TUI.
- Real builds, archive loading, private clone, login and compatibility evidence for
  every built-in image profile and supported platform.

These remain completion goals. They must not be silently dropped because the
current implementation supports the normal single-client path.
