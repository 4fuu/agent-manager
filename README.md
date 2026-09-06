# Agent Manager

Agent Manager is a terminal workspace for running coding agents in persistent
microsandbox microVMs. Organize work by Project and Session, with the Agent, shells
and file browser in horizontally arranged terminal columns. Every terminal in a
Session shares one sandbox; switching Sessions switches the whole workspace.

**Experimental.** Native Windows x64 with Windows Hypervisor Platform (WHP) is the
tested baseline; WSL is not required to run the application. Linux x64 runtime
checks have passed in WSL2 with KVM. Agent authentication and private repository
workflows still need end-to-end verification. See [tested behavior](docs/verification.md).

> [!WARNING]
> Use trusted repositories and image profiles. `.agents/setup` and Agent processes
> can read mapped configuration and credentials, and guest network access is
> unrestricted by Agent Manager. Writable mappings can change host files. Setup
> logs may contain secrets emitted by your scripts.

## Why Agent Manager

- **Independent workspaces:** create a Session from a Project and image. It clones
  the repository's default branch and runs `.agents/setup` automatically.
- **One sandbox per Session:** add shells and Yazi columns beside the Agent,
  navigate horizontally and adjust each column's width.
- **Persistent work:** a separate supervisor owns the guests and terminals.
  Closing the UI detaches; stopping a Session retains its disk. Deleting a Session
  removes its disk after confirmation.
- **Reusable image profiles:** name your own OCI images, download them from GHCR
  or another registry, or import local archives. Configure launch commands,
  environment and file or directory mappings once.
- **Agent-ready recipes:** URI Agent, Pi, Oh My Pi, Claude Code and Codex targets
  share Python, Node.js, Git, GitHub CLI, ripgrep, fd, Yazi and development tools.

## Quick start

### Requirements

- Windows x64 with Windows Hypervisor Platform, Linux x64/ARM64 with KVM and
  glibc 2.36+, or Apple Silicon macOS;
- a terminal with keyboard and mouse support;
- access to GitHub Releases and GHCR, and a GitHub repository for your first Session.

### Install

With Scoop on Windows:

```powershell
scoop bucket add agent-manager https://github.com/4fuu/agent-manager
scoop install agent-manager/agent-manager
```

Or use the standalone PowerShell installer:

```powershell
Invoke-WebRequest -UseBasicParsing https://raw.githubusercontent.com/4fuu/agent-manager/main/script/install.ps1 -OutFile "$env:TEMP\install-agent-manager.ps1"
powershell -ExecutionPolicy Bypass -File "$env:TEMP\install-agent-manager.ps1"
```

With Homebrew on Apple Silicon macOS:

```sh
brew tap 4fuu/agent-manager https://github.com/4fuu/agent-manager
brew install 4fuu/agent-manager/agent-manager
```

On Linux x64 or ARM64:

```sh
curl --proto '=https' --tlsv1.2 -LsSf https://raw.githubusercontent.com/4fuu/agent-manager/main/script/install.sh | sh
```

Installers verify the release archive checksum. Prebuilt bundles need no Go,
C compiler, Docker or WSL. The microsandbox FFI is embedded; its VM executable
and firmware are downloaded separately by `runtime-install`.
See [installation](docs/installation.md) for version selection, upgrades,
manual downloads and source builds.

### Start the manager

The managed login service commands require v2026.906.1 or later.
Keep the executable at a stable path when using them.

In a new terminal after installation:

```sh
agent-manager --version
agent-manager runtime-install
agent-manager doctor
agent-manager install
agent-manager
```

`install` registers the supervisor for login autostart for the current user and
starts it immediately. It is not a system boot service and normally needs no
administrator privileges. `runtime-install` and `doctor` remain separate runtime
prerequisites; [Windows setup](docs/usage.md#windows) explains how to enable WHP
when needed. See [installation](docs/installation.md#managed-login-service)
for platform requirements, upgrades, custom state directories and cleanup.

### Start your first Session

1. Press `i` to open **Images**, select an Agent profile and choose **Download**.
   Built-ins use dated GHCR images matching the application version. You can also
   add your own OCI reference and launch command or [build a local image](images/README.md).
2. Configure shared mappings with `g` or image-specific mappings in the image
   profile if your Agent or GitHub CLI needs host configuration. Mappings are
   optional and read-only by default.
3. Press `p` to create a **Project** with its name and GitHub repository URL.
4. Press `n` to create a **Session**, choosing the Project and image.

The Session is ready when cloning and setup finish and the selected command
appears in its terminal. Failed setup retains logs and offers retry and recovery.
Press `Ctrl+]` to return to manager navigation, `t` to add a shell, or `y` to open
Yazi. Use Left/Right to select a column and `+`/`-` to change its width.

## Documentation

| Goal | Guide |
| --- | --- |
| Install, upgrade, uninstall or build from source | [Installation](docs/installation.md) |
| Use Sessions, keyboard and mouse controls, image downloads or mappings | [Usage](docs/usage.md) |
| Build an Agent image or choose a local archive | [Image setup and contents](images/README.md) |
| Understand sandbox ownership, persistence and runtime boundaries | [Runtime](docs/runtime.md) |
| Check platform coverage or run native integration tests | [Verification](docs/verification.md) |
| Prepare a dated release, packages and GHCR images | [Release workflow](docs/release.md) |

## Development

Read [AGENTS.md](AGENTS.md) before changing the repository. It maps behavior to its
owning documentation and defines the required checks. `--fake` supports synthetic
UI and lifecycle testing; it does not verify sandbox isolation or Agent compatibility.

## License

Copyright (C) 2026 4fuu. Agent Manager is licensed under the
[GNU Affero General Public License, version 3 only](LICENSE)
(`AGPL-3.0-only`), without any warranty.

Commercial use is allowed. Redistribution of covered binaries requires access to
their Corresponding Source under the license. If you modify the program and let
users interact with that version remotely over a network, you must offer those
users its Corresponding Source. Private modifications do not by themselves require
publication. See the license for the complete terms.

Third-party dependencies and the Agents and tools installed in guest images retain
their own licenses. Merely using Agent Manager to work on a repository does not
make that repository subject to AGPL.
