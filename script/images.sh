#!/usr/bin/env bash
set -euo pipefail

target=${1:?usage: images.sh TARGET [VERSION]}
version=${2:-$(python3 script/release.py version)}
case "$target" in uri) agent=uri-agent;; pi) agent=pi;; omp) agent=omp;; claude) agent=claude;; codex) agent=codex;; *) echo "Unknown image target: $target" >&2; exit 1;; esac
image="ghcr.io/4fuu/agent-manager/$target:$version"
docker build -f images/Containerfile --target "$target" -t "$image" \
  --label org.opencontainers.image.source=https://github.com/4fuu/agent-manager \
  --label "org.opencontainers.image.version=$version" \
  --label "org.opencontainers.image.revision=$(git rev-parse HEAD)" .
docker run --rm "$image" bash -lc "set -e; $agent --version; python --version; node --version; python -m venv /tmp/check-venv; /tmp/check-venv/bin/pip --version; for tool in git gh rg fd fzf jq yazi tmux gcc make ssh curl; do command -v \"\$tool\"; done"
echo "Built and smoke-tested $image"
