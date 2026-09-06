# Installation and upgrades

## Platforms

| Bundle | Guest runtime requirement |
| --- | --- |
| Windows amd64 | Windows Hypervisor Platform and enabled hardware virtualization |
| Linux amd64 / arm64 | glibc 2.36+ and read/write access to `/dev/kvm` |
| macOS arm64 | Apple Silicon with Hypervisor.framework |

Windows builds run natively. Linux release binaries are built on Debian 12;
Alpine/musl hosts and Intel macOS are not supported. macOS distribution is not
Developer ID notarized. See [verification](verification.md) for the distinction
between build/installer checks and actual microVM coverage.

## Package managers

Windows:

```powershell
scoop bucket add agent-manager https://github.com/4fuu/agent-manager
scoop install agent-manager/agent-manager
```

Apple Silicon macOS:

```sh
brew tap 4fuu/agent-manager https://github.com/4fuu/agent-manager
brew install 4fuu/agent-manager/agent-manager
```

Release automation generates these manifests from the final archive checksums.

## Standalone installers

Download and review the installer before executing it if you want to inspect its
behavior. Both scripts accept `latest` (default), a dated version such as
`2026.906.0`, or its `v`-prefixed tag.

Windows PowerShell 5.1 or later:

```powershell
Invoke-WebRequest -UseBasicParsing https://raw.githubusercontent.com/4fuu/agent-manager/main/script/install.ps1 -OutFile "$env:TEMP\install-agent-manager.ps1"
powershell -ExecutionPolicy Bypass -File "$env:TEMP\install-agent-manager.ps1" -Version 2026.906.0
```

The default executable destination is
`%LOCALAPPDATA%\Programs\agent-manager\agent-manager.exe`. The script adds its
directory to the user PATH; open a new terminal afterward. Use `-InstallDir PATH`
or `AGENT_MANAGER_INSTALL_DIR` to change it. A failed download, checksum or binary
validation leaves the existing executable intact. Close the UI and supervisor
before upgrading; Windows may prevent replacement of a running executable.

Linux and Apple Silicon macOS:

```sh
curl --proto '=https' --tlsv1.2 -LsSf https://raw.githubusercontent.com/4fuu/agent-manager/main/script/install.sh -o /tmp/install-agent-manager.sh
sh /tmp/install-agent-manager.sh 2026.906.0
```

The Unix installer uses `~/.local/bin`, or `AGENT_MANAGER_INSTALL_DIR` / the
`--install-dir PATH` option. It installs a verified versioned directory under
`.agent-manager-versions` and atomically replaces the `agent-manager` symlink.
Older directories are retained for rollback; remove unused ones after stopping
old processes. Reinstalling the same version validates a fresh download and can
repair a damaged installation. Add the install directory to PATH when prompted.

The scripts use HTTPS and verify the exact archive entry in `SHA256SUMS` before
running the binary's version check. They do not install the runtime, create VMs,
modify project configuration or register a supervisor login service.

## Runtime and first launch

> [!NOTE]
> The `install`, `start`, `stop`, `status`, and `uninstall` service commands are
> available in v2026.906.1 and later. For v2026.906.0, run `serve` in a separate
> terminal or upgrade before following the managed-service instructions.

```sh
agent-manager --version
agent-manager runtime-install
agent-manager doctor
agent-manager install
agent-manager
```

`runtime-install` downloads the pinned
microsandbox runtime and firmware into `~/.microsandbox` (`MSB_HOME` overrides
this). The SDK's FFI library is embedded in the executable and extracted there
when first needed. No development toolchain is required to run a release bundle.
`doctor` checks the native runtime separately. A successful service installation
does not imply that the hypervisor or runtime is ready.

Use the [first Session walkthrough](../README.md#start-your-first-session), then
see [usage](usage.md) for image downloads and configuration mappings.

## Managed login service

The following commands manage one current-user registration:

```sh
agent-manager install [--state DIR]
agent-manager start [--state DIR]
agent-manager stop [--state DIR]
agent-manager status [--state DIR]
agent-manager uninstall [--state DIR]
```

Use exactly the same `--state DIR` on every lifecycle command and when launching
the UI. Registrations are keyed by the canonical state path, so different state
directories create independent registrations. Omitting `--state` consistently
uses the platform default.

- `install` registers login autostart for the current user and starts the
  supervisor immediately. Reinstalling gracefully stops the existing supervisor,
  refreshes the registered executable path and captured `MSB_HOME`, then starts it.
- `start` requires an installed registration and waits for the private supervisor
  IPC endpoint to become ready.
- `stop` gracefully stops the supervisor and leaves login autostart installed.
  Existing guests are detached and their disks are retained.
- `status` reports whether a registration exists, native service state, IPC
  readiness, the canonical state directory and the lifecycle log path.
- `uninstall` stops the supervisor and removes its native registration. It retains
  manager state, runtime caches, images and guest disks, and does not remove the
  `agent-manager` program.

The registration captures `MSB_HOME` at installation. Other variables from the
interactive shell are not promised in the service environment. The lifecycle log
is `STATE/supervisor-service.log`; it is overwritten on each service start and
never contains persisted guest terminal output. `serve` remains available for a
foreground supervisor. `--fake` is test infrastructure accepted only by `serve`
and `install`; an install records that choice in the registration.

Keep the registered executable at a stable path. Before upgrading, stop the
service. After any upgrade or executable move—including versioned package-manager
or Unix installer paths—rerun `agent-manager install` to refresh the registration.
Before removing the application, run `agent-manager uninstall` with the matching
state directory.

Native registration uses:

- **Windows:** an interactive-user Task Scheduler task with least privilege and a
  hidden PowerShell/WMI launcher. Task Scheduler, Windows PowerShell and local WMI
  process creation must be available. No elevation is normally required, although
  local policy can deny these operations. The user must be logged in for it to run.
- **Linux:** a `systemd --user` service. A working user systemd session and user
  bus are required.
- **macOS:** a LaunchAgent in the `gui/<uid>` domain. The user must have a logged-in
  GUI session.

These are login services, not machine boot services. Login, reboot and native
runtime behavior require platform-specific checks separate from unit tests. If
you previously enabled the legacy manual `contrib/agent-manager.service`, stop and
disable it before installing the managed service so both do not contend for state.

## Manual archives and source builds

[GitHub Releases](https://github.com/4fuu/agent-manager/releases) contains
`agent-manager-VERSION-PLATFORM.zip` (Windows) or `.tar.gz` (Unix), `SHA256SUMS`
and `images.json` with pinned GHCR digests. Download the matching program archive
and compare its SHA-256 with the manifest, then extract it to a directory on PATH.
Image tar files are not release assets.

For a source build, install Go 1.25.1+ and a native C toolchain, then run from the
repository root:

```sh
go build ./cmd/agent-manager
```

CGO must be enabled (`CGO_ENABLED=1`; in PowerShell use `$env:CGO_ENABLED='1'`).
Native release packaging uses Python 3.11+ and `python script/release.py build`.

## Upgrade and uninstall

Stop the supervisor before upgrading, then use `scoop update agent-manager`,
`brew upgrade agent-manager`, or rerun the standalone installer. For a managed
login service, rerun `agent-manager install` afterward to record the new executable
path and restart it. Existing image profiles and Sessions retain their resolved
configuration; an application upgrade does not silently retag them.

Remove the managed registration before uninstalling the application. Then use
your package manager's uninstall command, or remove the standalone executable
(Windows) / symlink and its `.agent-manager-versions` directory (Unix). Remove the
standalone PATH entry if no longer needed. Application removal intentionally
retains manager state, runtime caches and guest disks. Delete Sessions in the UI
when you intend to remove their disks; see [state locations](usage.md#supervisor-and-state).
