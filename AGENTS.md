# Agent Manager

Go TUI and independent supervisor for microsandbox guests. URI Agent runs in the
guest, not on the host. Keep commands behind `internal/backend`; never introduce
a host-execution fallback. `--fake` is synthetic test infrastructure, not isolation.

Run `gofmt -w cmd internal`, `go test ./...`, `go test -race ./...`,
`go vet ./...`, and `go build ./cmd/agent-manager` before committing.
For UI changes, exercise the actual TUI with both keys and mouse; inspect rendered
Project forms, failed setup logs, and terminal focus/resize states.

State JSON is versioned and atomically replaced. The supervisor directory and
socket are private to the user. Preserve runtime identities; never recreate a
missing known instance automatically. Stop retains the disk; delete removes it.
Config mappings are individual files, read-only by default. Do not log credentials
or introduce home-directory mounts. Guest terminal content is not persisted as logs.

See `docs/runtime.md` for upstream contracts and unverified runtime boundaries.
