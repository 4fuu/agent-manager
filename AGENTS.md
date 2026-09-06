# AGENTS.md

Guidance for coding agents working anywhere in this repository. This file is the
entry point; detailed contracts belong to the linked documents.

## Project contract

Agent Manager is a Go TUI and independent supervisor for persistent microsandbox
guests. The hierarchy is Project → Session. Each Session owns one sandbox shared
by all its Agent, shell, Yazi and recovery terminals. Creating a Session clones
the repository's default branch and runs `.agents/setup` before starting the Agent.

Native Windows support is mandatory. Keep guest execution and image operations
behind `internal/backend`; never introduce a host-execution or WSL fallback.
`--fake` is synthetic test infrastructure, not isolation or runtime verification.

## Read before changing

Read the focused documents that own the affected behavior:

| Area | Authoritative detail |
| --- | --- |
| Projects, Sessions, UI controls, image profiles and mappings | [Usage](docs/usage.md) |
| Supervisor ownership, guest lifecycle, image transfers, state and upstream contracts | [Runtime](docs/runtime.md) |
| Image recipes, installed tools, build and archive import | [Image setup](images/README.md) |
| Installers, upgrades, bundles and source builds | [Installation](docs/installation.md) |
| Date versions, CI, package metadata and GHCR publication | [Release workflow](docs/release.md) |
| Platform evidence, required runtime checks and unverified boundaries | [Verification](docs/verification.md) |

Read all applicable documents for cross-domain changes. CLI help defines the
command-line contract. Keep implementation, tests, help and documentation consistent.

## Working rules

- Put behavior in the module that owns it: `internal/ui` for interaction,
  `internal/supervisor` for lifecycle and jobs, `internal/manager` for durable
  state, and `internal/backend` for runtime operations.
- Make the smallest complete change. Follow references when removing behavior;
  leave unrelated refactors and speculative configurability alone.
- Preserve runtime identities. Never recreate a missing known instance
  automatically. Stop retains the disk; delete removes it.
- Keep state JSON versioned and atomically replaced. Protect the supervisor
  directory and IPC endpoint for the current user.
- Mappings are explicit files or directories, read-only by default with opt-in
  writes. Do not add implicit home-directory mounts. Credentials and executable
  configuration are trusted guest inputs; do not suppress setup or its logs merely
  because a mapped directory holds secrets. Never deliberately log credentials.
- Do not persist guest terminal content as manager logs or commit credentials,
  runtime state, `.amp/`, generated binaries or image archives.

## Verification

Add focused tests beside changed behavior. Before committing code changes, run:

```sh
gofmt -w cmd internal
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/agent-manager
```

For installer or release changes, also run
`python -m unittest discover -s script -p 'test_*.py' -v`, validate workflow YAML
with actionlint, and exercise the affected native bundle or installer. Keep all
release handling in `script/`; generate package hashes from real final archives.

For UI changes, exercise the actual TUI with keyboard and mouse and inspect its
rendered output. Cover affected forms and failure states, terminal focus and
resize. For runtime changes, run the applicable opt-in native checks in
[verification](docs/verification.md); WSL checks do not replace native Windows checks.
Report unavailable or unverified coverage explicitly.

Documentation-only changes require checking commands against the implementation,
relative links and consistency with owning documents; they do not require the Go
suite. Default tests must not require live credentials or external network access.

## Documentation changes

- Keep `README.md` focused on adoption, first success, critical warnings and navigation.
- Update the owning detailed document when behavior changes; avoid duplicating
  mutable reference details in the README or this file.
- Update CLI help when public commands change.
- Record real runtime evidence separately from synthetic tests. Build or startup
  success does not establish authenticated Agent compatibility.
- Finish every release by writing and verifying the GitHub Release Notes using
  the [required format and completion checks](docs/release.md#release-notes-final-required-step).
  An automated commit summary or green workflow is not the final release handoff.
