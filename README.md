# Agent Manager

A Go terminal workspace for running coding agents in persistent microsandbox
microVMs. Projects contain a GitHub repository; Sessions are independent guest
disks created from a Project and an image profile. A separate supervisor owns the
guests and their terminals, so closing the UI only detaches.

**Experimental.** Native Windows x64 with Windows Hypervisor Platform (WHP) is the
tested baseline. Windows operation is native and does not fall back to WSL or host
execution. Image builds, image import, private-repository clone, Agent login and
Agent compatibility remain unverified. See [verification](docs/verification.md).

## Run on Windows

Requirements: WHP, Go 1.25.1+, CGO and a native C toolchain. Use Windows Terminal
or another mouse-capable terminal.

```powershell
$env:CGO_ENABLED = '1'
go build -o agent-manager.exe ./cmd/agent-manager
.\agent-manager.exe runtime-install
.\agent-manager.exe doctor
.\agent-manager.exe serve
# In another PowerShell window:
.\agent-manager.exe
```

No images are published and Agent Manager does not install an OCI builder. Build
and import a local image first; [image setup](images/README.md) lists the supplied
URI, Pi, OMP, Claude and Codex targets.

In the UI, create a **Project** (name and repository), then a **Session** (Project
and image). A Session clones the repository's default branch, runs `.agents/setup`
when present, and starts the selected Agent. Setup failures retain logs and expose
retry and recovery actions. `Ctrl+]` returns terminal focus to the manager.

## Documentation

- [Controls, Projects, Sessions, mappings and Windows setup](docs/usage.md)
- [Runtime, lifecycle, images and state](docs/runtime.md)
- [Tested behavior and opt-in checks](docs/verification.md)
- [Product goals and known design gaps](DESIGN.md)

Only explicitly configured host files or directories are mapped; mappings are
read-only by default. Project repositories are treated as trusted: mapped config
and credentials are available to `.agents/setup` and Agent processes. Directory
output is logged normally and may contain credentials; redaction covers known
values read from individual mapped files, not recursive directory contents.

Stop retains a Session disk. Delete removes it after confirmation while retaining
manager logs and host mapping sources. `--fake` is synthetic UI/lifecycle test
infrastructure and is not an isolation boundary.
