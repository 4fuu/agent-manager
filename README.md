# Agent Manager

A Go terminal UI for running **URI Agent inside persistent microsandbox microVMs**.
Save a GitHub repository, OCI image, launch command, ref and config-file mappings as
a Project; launch independent instances from it. An independent supervisor owns
guest terminals, so quitting the UI detaches instead of stopping your work.

**Experimental.** The microsandbox v0.6.17 adapter compiles against the official Go
SDK, but live image boot, file-mount enforcement and URI Agent operation have not
been verified in this development orb: it has no `/dev/kvm`. Lifecycle and terminal
tests use an explicitly synthetic backend. Do not treat those tests as proof of
microVM isolation. See [runtime contracts and limitations](docs/runtime.md).

## Run

Requirements: Linux with accessible KVM, Go 1.25.1+, a C toolchain, and a terminal
with mouse support. Use at least 100×35 cells for comfortable forms. macOS arm64
is an upstream SDK target but has not been tested here; Windows is not supported
by this manager's Unix-socket supervisor.

```sh
go build -o agent-manager ./cmd/agent-manager
./agent-manager doctor
./agent-manager runtime-install
```

Build/import the supplied [URI Agent image](docs/runtime.md#build-the-image), or
select an existing OCI image containing URI Agent, git, bash, tmux and flock.
The default `uri-agent-manager:2026.904.3` is a **local build tag**, not a published
image. Do not launch it before importing the recipe's output.

Start the supervisor in a separate terminal or install the
[user service](docs/usage.md#supervisor-service):

```sh
./agent-manager serve
# In another terminal:
./agent-manager
```

Click **Add** (F1), save a Project, select it, and click **New instance** (F3).
The right pane streams clone/setup logs and switches to the guest terminal after
successful setup. Setup failure leaves logs and **Retry setup** / **Shell** actions.
An absent `.agents/setup` requires no setup. **Ctrl+]** returns from the terminal
to manager controls; **Quit UI** leaves the supervisor and guests running.

## Try the UI without KVM

```sh
./agent-manager serve --fake --state /tmp/agent-manager-demo
# In another terminal:
./agent-manager ui --state /tmp/agent-manager-demo
```

Use any Project GitHub URL; fake mode never clones or executes commands. Image
`fixture:fail-setup` simulates a first setup failure, then succeeds on retry.
The embedded terminal identifies itself as a fixture; it is not URI Agent.

## Configuration and operation

- [Controls, Projects, mappings and private GitHub authentication](docs/usage.md)
- [Image recipe, durable-state semantics and upstream compatibility](docs/runtime.md)
- [Behavior coverage and verification](docs/verification.md)

Config mappings expose only explicitly selected files, read-only by default.
Mapped credentials are available to guest code; use narrowly scoped credentials
and trusted repositories. Stop retains the guest disk. Delete removes the entire
instance disk after confirmation, but retains redacted setup logs and host files.

For development, `.agents/setup` installs Go and builds the manager. Verify with
`go test ./...`, `go test -race ./...`, `go vet ./...`, and
`gofmt -l cmd internal` (no output expected).
