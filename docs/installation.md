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
modify project configuration or start a supervisor.

## Runtime and first launch

```sh
agent-manager --version
agent-manager runtime-install
agent-manager doctor
agent-manager serve
```

Run `agent-manager` in a second terminal. `runtime-install` downloads the pinned
microsandbox runtime and firmware into `~/.microsandbox` (`MSB_HOME` overrides
this). The SDK's FFI library is embedded in the executable and extracted there
when first needed. No development toolchain is required to run a release bundle.

Use the [first Session walkthrough](../README.md#start-your-first-session), then
see [usage](usage.md) for image downloads and configuration mappings.

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
`brew upgrade agent-manager`, or rerun the standalone installer. Start the new
supervisor and UI together. Existing image profiles and Sessions retain their
resolved configuration; an application upgrade does not silently retag them.

Use your package manager's uninstall command, or remove the standalone executable
(Windows) / symlink and its `.agent-manager-versions` directory (Unix). Remove the
standalone PATH entry if no longer needed. Application removal intentionally
retains manager state, runtime caches and guest disks. Delete Sessions in the UI
when you intend to remove their disks; see [state locations](usage.md#supervisor-and-state).
