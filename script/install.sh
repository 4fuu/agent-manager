#!/bin/sh
set -eu

repo=4fuu/agent-manager
version=latest
install_dir=${AGENT_MANAGER_INSTALL_DIR:-"$HOME/.local/bin"}
fixture=${AGENT_MANAGER_FIXTURE_DIR:-}

usage() { echo "usage: install.sh [latest|VERSION] [--install-dir DIR]" >&2; exit 2; }
while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-dir) [ "$#" -ge 2 ] || usage; install_dir=$2; shift 2 ;;
    --install-dir=*) install_dir=${1#*=}; shift ;;
    -h|--help) usage ;;
    *) [ "$version" = latest ] || usage; version=$1; shift ;;
  esac
done

need() { command -v "$1" >/dev/null 2>&1 || { echo "error: required command not found: $1" >&2; exit 1; }; }
need uname; need mktemp; need awk; need tar
if [ -n "$fixture" ]; then
  [ "${fixture#/}" != "$fixture" ] || { echo "error: AGENT_MANAGER_FIXTURE_DIR must be an absolute local path" >&2; exit 1; }
  if [ "$version" = latest ]; then version=$(cat "$fixture/LATEST") || exit 1; fi
else
  need curl
  if [ "$version" = latest ]; then
    latest_url=$(curl --proto '=https' --tlsv1.2 -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")
    version=${latest_url##*/}
  fi
fi
version=${version#v}
echo "$version" | awk -F. 'NF==3 && $1 ~ /^[0-9][0-9][0-9][0-9]$/ && $2 ~ /^[1-9][0-9][0-9][0-9]?$/ && $3 ~ /^[0-9]+$/ && (length($3)==1 || substr($3,1,1)!="0") { d=$2%100; m=int($2/100); days=31; if(m==4 || m==6 || m==9 || m==11) days=30; if(m==2) days=28+($1%4==0 && ($1%100!=0 || $1%400==0)); valid=(d>=1 && d<=days && m>=1 && m<=12) } END { exit !valid }' || {
  echo "error: invalid version '$version' (expected YYYY.MDD.REVISION)" >&2; exit 1;
}

os=$(uname -s); machine=$(uname -m)
case "$os/$machine" in
  Linux/x86_64|Linux/amd64) target=linux-amd64 ;;
  Linux/aarch64|Linux/arm64) target=linux-arm64 ;;
  Darwin/arm64) target=darwin-arm64 ;;
  *) echo "error: unsupported target: $os/$machine" >&2; exit 1 ;;
esac
archive="agent-manager-$version-$target.tar.gz"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/agent-manager-install.XXXXXX")
new=; linktmp=
trap 'rm -rf "$tmp"; [ -z "$new" ] || rm -rf "$new"; [ -z "$linktmp" ] || rm -f "$linktmp"' EXIT
trap 'exit 1' HUP INT TERM
if [ -n "$fixture" ]; then
  cp "$fixture/$archive" "$tmp/$archive"
  cp "$fixture/SHA256SUMS" "$tmp/SHA256SUMS"
else
  base="https://github.com/$repo/releases/download/v$version"
  curl -fL --proto '=https' --tlsv1.2 -o "$tmp/$archive" "$base/$archive"
  curl -fL --proto '=https' --tlsv1.2 -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"
fi
line=$(awk -v f="$archive" '$2==f { n++; hash=$1 } END { if(n==1 && hash ~ /^[0-9A-Fa-f]+$/ && length(hash)==64) print hash }' "$tmp/SHA256SUMS")
[ -n "$line" ] || { echo "error: malformed checksum manifest or missing exact entry for $archive" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
else echo "error: sha256sum or shasum is required" >&2; exit 1; fi
[ "$actual" = "$line" ] || { echo "error: checksum verification failed for $archive" >&2; exit 1; }

mkdir "$tmp/unpack"
tar -xzf "$tmp/$archive" -C "$tmp/unpack"
[ -f "$tmp/unpack/agent-manager" ] && [ -f "$tmp/unpack/README.md" ] && [ -d "$tmp/unpack/docs" ] || {
  echo "error: archive layout is invalid" >&2; exit 1;
}
chmod +x "$tmp/unpack/agent-manager"
reported=$("$tmp/unpack/agent-manager" --version 2>/dev/null) || { echo "error: downloaded executable failed its version test" >&2; exit 1; }
case "$reported" in "agent-manager $version"|"agent-manager $version ("*) ;; *) echo "error: downloaded executable reports the wrong version" >&2; exit 1;; esac

mkdir -p "$install_dir/.agent-manager-versions"
new=$(mktemp -d "$install_dir/.agent-manager-versions/$version.XXXXXX")
cp -R "$tmp/unpack/." "$new/"
linktmp="$install_dir/.agent-manager-link-$$"
ln -s ".agent-manager-versions/${new##*/}/agent-manager" "$linktmp"
mv -f "$linktmp" "$install_dir/agent-manager"
new=; linktmp=
echo "Installed agent-manager $version to $install_dir/agent-manager"
case ":$PATH:" in *":$install_dir:"*) ;; *) echo "Add $install_dir to PATH.";; esac
echo "Next: agent-manager runtime-install"
echo "      agent-manager doctor"
echo "      agent-manager serve"
