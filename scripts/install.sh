#!/bin/sh
set -eu

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "Unsupported operating system" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported CPU architecture" >&2; exit 1 ;;
esac

asset=$(printf 'english-guard_%s_%s.tar.gz' "$os" "$arch")
base=https://github.com/rebrudiy/codex-english-guard/releases/latest/download
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

curl -fsSL "$base/$asset" -o "$temporary/$asset"
curl -fsSL "$base/checksums.txt" -o "$temporary/checksums.txt"
expected=$(awk -v name="$asset" '$2 == name { print $1 }' "$temporary/checksums.txt")
if [ -z "$expected" ]; then
  echo "Release checksum for $asset is missing" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$temporary/$asset" | cut -d ' ' -f1)
else
  actual=$(shasum -a 256 "$temporary/$asset" | cut -d ' ' -f1)
fi
if [ "$actual" != "$expected" ]; then
  echo "Release checksum mismatch" >&2
  exit 1
fi

tar -xzf "$temporary/$asset" -C "$temporary"
"$temporary/english-guard" install "$@"
