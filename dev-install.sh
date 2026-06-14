#!/usr/bin/env sh
# dev-install.sh — build from source and wire up the shell hook, for local
# testing. No GitHub release required (unlike install.sh, which downloads).
#
#   ./dev-install.sh
#
# Requires Go on PATH. If you used the repo's local Go install, first run:
#   export PATH="$HOME/.local/go/bin:$PATH"
set -e

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BIN_DIR="${HELPME_BIN_DIR:-$HOME/.local/bin}"
HOOK_DIR="${HELPME_HOOK_DIR:-$HOME/.config/helpme}"
mkdir -p "$BIN_DIR" "$HOOK_DIR"

if ! command -v go >/dev/null 2>&1; then
  echo "Go not found on PATH." >&2
  echo "If you used the repo's local Go install, run this first:" >&2
  echo '  export PATH="$HOME/.local/go/bin:$PATH"' >&2
  echo "Otherwise install Go: https://go.dev/dl/" >&2
  exit 1
fi

echo "Building helpme-bin -> $BIN_DIR/helpme-bin"
( cd "$REPO_DIR" && go build -o "$BIN_DIR/helpme-bin" . )

shell_name=$(basename "${SHELL:-}")
case "$shell_name" in
  zsh)  rc="$HOME/.zshrc";  hookfile="$HOOK_DIR/helpme.zsh" ;;
  bash) rc="$HOME/.bashrc"; hookfile="$HOOK_DIR/helpme.bash" ;;
  *)
    echo "Built. Source a hook from hooks/ manually for shell '$shell_name'."
    exit 0
    ;;
esac

"$BIN_DIR/helpme-bin" --print-hook "$shell_name" > "$hookfile"

line="source \"$hookfile\""
if ! grep -qsF "$line" "$rc"; then
  printf '\n# helpme\n%s\n' "$line" >> "$rc"
  echo "Added helpme hook to $rc"
else
  echo "helpme hook already present in $rc"
fi

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "NOTE: add $BIN_DIR to PATH (e.g. export PATH=\"$BIN_DIR:\$PATH\")." ;;
esac

echo
echo "Done. Open a new shell (or: source $rc), then run:  helpme setup"
