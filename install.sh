#!/usr/bin/env sh
# helpme installer.
#
# Builds the helpme binary (requires Go) into ~/.local/bin and wires the shell
# wrapper into your rc file. Idempotent — safe to re-run.
#
#   ./install.sh
#
# Override the binary location with HELPME_BIN_DIR.
set -e

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BIN_DIR="${HELPME_BIN_DIR:-$HOME/.local/bin}"
mkdir -p "$BIN_DIR"

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go is required to build helpme (https://go.dev/dl/)." >&2
  exit 1
fi

echo "Building helpme-bin -> $BIN_DIR/helpme-bin"
( cd "$REPO_DIR" && go build -o "$BIN_DIR/helpme-bin" . )

# Choose the hook matching the current shell.
shell_name=$(basename "${SHELL:-}")
case "$shell_name" in
  zsh)  hook="$REPO_DIR/hooks/helpme.zsh";  rc="$HOME/.zshrc" ;;
  bash) hook="$REPO_DIR/hooks/helpme.bash"; rc="$HOME/.bashrc" ;;
  *)
    echo "Unrecognized shell '$shell_name'."
    echo "Source the matching file in hooks/ from your shell rc manually."
    exit 0
    ;;
esac

line="source \"$hook\""
if ! grep -qsF "$line" "$rc"; then
  printf '\n# helpme\n%s\n' "$line" >> "$rc"
  echo "Added helpme hook to $rc"
else
  echo "helpme hook already present in $rc"
fi

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "NOTE: $BIN_DIR is not on your PATH — add it so 'helpme-bin' is found." ;;
esac

echo
echo "Done. Open a new shell (or: source $rc), then set your provider:"
echo "  export HELPME_PROVIDER=anthropic        # or openai | openrouter | custom"
echo "  export HELPME_API_KEY=sk-ant-..."
echo "Then try:  helpme find -f myfile.txt"
