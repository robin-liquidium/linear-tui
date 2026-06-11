#!/usr/bin/env sh
set -eu

# Install linear-tui and its short lt command into the user's local bin directory.
prefix="${PREFIX:-$HOME/.local}"
bindir="$prefix/bin"
binary="$bindir/linear-tui"
shortcut="$bindir/lt"

mkdir -p "$bindir"
go build -o "$binary" ./cmd/linear-tui

if [ -e "$shortcut" ] && [ ! -L "$shortcut" ]; then
  echo "Refusing to replace existing non-symlink command: $shortcut" >&2
  exit 1
fi

ln -sfn "linear-tui" "$shortcut"

echo "Installed linear-tui -> $binary"
echo "Installed lt -> $shortcut"
case ":$PATH:" in
  *":$bindir:"*) ;;
  *) echo "Note: $bindir is not on PATH, so add it or run with PREFIX set to a PATH directory." ;;
esac
