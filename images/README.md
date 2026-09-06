# Agent images

`Containerfile` defines five local build targets:

```sh
docker build -f images/Containerfile --target uri    -t uri-agent-manager:2026.904.3 .
docker build -f images/Containerfile --target pi     -t uri-agent-manager-pi:local .
docker build -f images/Containerfile --target omp    -t uri-agent-manager-omp:local .
docker build -f images/Containerfile --target claude -t uri-agent-manager-claude:local .
docker build -f images/Containerfile --target codex  -t uri-agent-manager-codex:local .
```

Agent Manager does not install Docker/Podman and these tags are not published.
Builds have not yet been verified. The common base provides Git, GitHub CLI,
ripgrep, fd, Yazi, Bash, tmux, curl, Node.js, Python and build tools. Target Agent
versions can be set with `URI_VERSION`, `PI_VERSION`, `OMP_VERSION`,
`CLAUDE_VERSION` and `CODEX_VERSION`.

A profile may instead use an absolute OCI archive path. Session creation loads it
automatically through microsandbox `Image.Load`; users do not need to run a
separate manager import command. An OCI reference remains available for an image
already visible to microsandbox.

All profiles can receive explicit shared/profile file or directory mappings.
They are read-only by default; `rw` is opt-in. Nothing from the host is mounted
implicitly. Mapped credentials and executable config hooks are available to setup
and Agent processes.
