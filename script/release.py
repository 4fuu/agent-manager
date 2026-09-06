#!/usr/bin/env python3
"""Validate dated versions, build native bundles, and generate release metadata."""

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
VERSION_FILE = ROOT / "internal/buildinfo/version.txt"
TARGETS = ("windows-amd64", "linux-amd64", "linux-arm64", "darwin-arm64")
IMAGES = ("uri", "pi", "omp", "claude", "codex")
REPO = "4fuu/agent-manager"


def validate_version(value):
    match = re.fullmatch(r"(\d{4})\.(\d{3,4})\.(0|[1-9]\d*)", value)
    if not match:
        raise ValueError("expected YYYY.MDD.REVISION, e.g. 2026.906.0")
    year, md, _ = match.groups()
    day = dt.date(int(year), int(md[:-2]), int(md[-2:]))
    if md != f"{day.month}{day.day:02}":
        raise ValueError("month must be unpadded and day must be two digits")
    return value


def version():
    return validate_version(VERSION_FILE.read_text().strip())


def archive_name(v, target):
    suffix = "zip" if target.startswith("windows-") else "tar.gz"
    return f"agent-manager-{v}-{target}.{suffix}"


def run(*args, **kwargs):
    return subprocess.run(args, cwd=ROOT, check=True, **kwargs)


def build(out):
    v = version()
    env = dict(os.environ, CGO_ENABLED="1")
    target = "-".join(run("go", "env", "GOOS", "GOARCH", capture_output=True, text=True).stdout.split())
    if target not in TARGETS:
        raise ValueError(f"unsupported release target: {target}")
    commit = run("git", "rev-parse", "HEAD", capture_output=True, text=True).stdout.strip()
    flags = f"-s -w -X github.com/4fuu/agent-manager/internal/buildinfo.Commit={commit}"
    if target.startswith("windows-"):
        flags += " -extldflags=-static"
    out.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="agent-manager-build-") as tmp:
        stage = Path(tmp)
        exe = stage / ("agent-manager.exe" if target.startswith("windows-") else "agent-manager")
        run("go", "build", "-trimpath", "-ldflags", flags, "-o", str(exe), "./cmd/agent-manager", env=env)
        reported = run(str(exe), "--version", capture_output=True, text=True).stdout
        if not reported.startswith(f"agent-manager {v} ("):
            raise ValueError(f"unexpected binary version: {reported}")
        for name in ("LICENSE", "README.md", "AGENTS.md"):
            shutil.copy2(ROOT / name, stage / name)
        for name in ("docs", "images", "contrib"):
            shutil.copytree(ROOT / name, stage / name)
        destination = out / archive_name(v, target)
        if target.startswith("windows-"):
            with zipfile.ZipFile(destination, "w", zipfile.ZIP_DEFLATED) as archive:
                for file in sorted(stage.rglob("*")):
                    if file.is_file():
                        archive.write(file, file.relative_to(stage))
        else:
            with tarfile.open(destination, "w:gz") as archive:
                for file in sorted(stage.iterdir()):
                    archive.add(file, arcname=file.name)
        print(destination)


def checksums(out):
    expected = {archive_name(version(), t) for t in TARGETS}
    actual = {p.name for p in out.glob("agent-manager-*") if p.is_file()}
    if actual != expected:
        raise ValueError(f"archive set mismatch: missing {expected - actual}, extra {actual - expected}")
    lines = []
    for name in sorted(expected):
        with (out / name).open("rb") as f:
            digest = hashlib.file_digest(f, "sha256").hexdigest()
        lines.append(f"{digest}  {name}\n")
    (out / "SHA256SUMS").write_text("".join(lines), encoding="utf-8", newline="\n")


def read_checksums(path, v):
    hashes = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r"([a-f0-9]{64})  ([^ /\\]+)", line)
        if not match or match[2] in hashes:
            raise ValueError("malformed or duplicate checksum entry")
        hashes[match[2]] = match[1]
    if set(hashes) != {archive_name(v, t) for t in TARGETS}:
        raise ValueError("checksums must contain exactly the four release archives")
    return hashes


def metadata(out):
    v = version()
    hashes = read_checksums(out / "SHA256SUMS", v)
    base = f"https://github.com/{REPO}/releases/download/v{v}"
    win = archive_name(v, "windows-amd64")
    scoop = {
        "version": v, "description": "Persistent sandbox workspaces for terminal coding agents",
        "homepage": f"https://github.com/{REPO}",
        "license": "AGPL-3.0-only",
        "notes": "Enable Windows Hypervisor Platform, then run agent-manager runtime-install and agent-manager doctor. Stop the supervisor before updating.",
        "architecture": {"64bit": {"url": f"{base}/{win}", "hash": hashes[win]}},
        "bin": "agent-manager.exe",
        "checkver": {"github": f"https://github.com/{REPO}"},
        "autoupdate": {"architecture": {"64bit": {
            "url": f"https://github.com/{REPO}/releases/download/v$version/agent-manager-$version-windows-amd64.zip"
        }}},
    }
    (ROOT / "bucket").mkdir(exist_ok=True)
    (ROOT / "bucket/agent-manager.json").write_text(json.dumps(scoop, indent=4) + "\n", encoding="utf-8")
    mac = archive_name(v, "darwin-arm64")
    formula = f'''class AgentManager < Formula
  desc "Persistent sandbox workspaces for terminal coding agents"
  homepage "https://github.com/{REPO}"
  license "AGPL-3.0-only"
  url "{base}/{mac}"
  sha256 "{hashes[mac]}"
  version "{v}"

  depends_on :macos
  depends_on arch: :arm64

  def install
    bin.install "agent-manager"
    doc.install "LICENSE", "README.md", "docs", "images"
  end

  def caveats
    <<~EOS
      Run agent-manager runtime-install, then agent-manager doctor.
      Stop the supervisor before upgrading. Guest boot requires Apple Silicon.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{{bin}}/agent-manager --version")
  end
end
'''
    (ROOT / "Formula").mkdir(exist_ok=True)
    (ROOT / "Formula/agent-manager.rb").write_text(formula, encoding="utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    vp = sub.add_parser("version")
    vp.add_argument("--set")
    vp.add_argument("--today", action="store_true")
    for name in ("build", "checksums", "metadata"):
        sub.add_parser(name).add_argument("--out", type=Path, default=ROOT / "dist")
    args = parser.parse_args()
    if args.command == "version":
        v = validate_version(args.set) if args.set else version()
        if args.today:
            today = dt.datetime.now(dt.timezone(dt.timedelta(hours=8))).date()
            prefix = f"{today.year}.{today.month}{today.day:02}."
            if not v.startswith(prefix):
                raise ValueError(f"release must use today's Asia/Hong_Kong date: {prefix}REVISION")
        if args.set:
            VERSION_FILE.write_text(v + "\n", encoding="utf-8")
        print(v)
    else:
        globals()[args.command](args.out.resolve())


if __name__ == "__main__":
    main()
