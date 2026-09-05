# Behavior coverage

The executable tests use a synthetic backend which never spawns host commands.
It persists fake VM markers and emits labeled terminal output. Real microVM
execution requires the KVM validation described in [runtime.md](runtime.md).

| Capability | Owner | Verification |
| --- | --- | --- |
| Reusable Projects, global mapping overrides, private metadata | `manager`, `supervisor` | Mapping safety, validation, persistence and snapshot tests |
| Independent instance image/ref/disk identities | `supervisor`, microsandbox adapter | Independent-instances and missing-identity tests |
| Clone → setup → agent, no agent on failure | `supervisor` | Failure/shell/retry lifecycle test; guest command contract assertions |
| Setup only on new environments or explicit retry | Guest marker + manager `SetupDone` | Stop/resume log comparison and reconnect tests |
| Stop differs from delete | Backend `Control`, persistent runtime handle | Disk marker retention/removal; delete-before-boot test |
| Stop during setup | Supervisor cancellation | Blocked-setup cancellation test; completion remains false |
| Background ownership / UI reattach | Unix RPC server and guest tmux | Independent client frame requests and supervisor reopen test |
| Exclusive same-user IPC | Unix socket + advisory lock | Private-socket, client-reconnect and duplicate-supervisor tests |
| Key, Unicode paste, mouse mode routing and resize | Charm VT + tview terminal | Recorded PTY bytes, local coordinates, resize and concurrent frame tests |
| Terminal shutdown | Supervisor terminal wrapper | Race checks and blocked device-query close test |
| Secret filtering | Streaming line redactor | Split-chunk JSON credential redaction test |
| Atomic metadata and restricted permissions | Versioned state writer | Save/load/version/mode test |

Commands:

```sh
gofmt -l cmd internal
go test ./...
go test -race ./...
go vet ./...
go build -o agent-manager ./cmd/agent-manager
bash -n .agents/setup
sh -n images/manager-git-askpass
```

For manual UI verification, start `serve --fake` in a separate service/terminal,
then run the real UI in a terminal. Add/edit a Project with both mouse and keys,
select image `fixture:fail-setup`, launch, inspect the failed logs, open recovery,
retry, type/paste and resize the embedded terminal. Quit/reopen the UI and ensure
the instance remains running. Stop/resume without duplicate setup. Confirm and
cancel deletion. Use at least 100×35 terminal cells for form coverage.
