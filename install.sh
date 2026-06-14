#!/usr/bin/env sh
# helpme installer — downloads a prebuilt binary. No Go toolchain required.
#
#   curl -fsSL https://raw.githubusercontent.com/<owner>/helpme/main/install.sh | sh
#
# Detects your OS/arch, fetches the matching binary into ~/.local/bin, writes
# the shell hook straight from the binary (so it always matches), and adds one
# `source` line to your rc. Idempotent — safe to re-run.
#
# Env overrides:
#   HELPME_REPO              GitHub owner/repo            (default: alanmun/helpme)
#   HELPME_VERSION           release tag or "latest"      (default: latest)
#   HELPME_BIN_DIR           where helpme-bin goes        (default: ~/.local/bin)
#   HELPME_HOOK_DIR          where the hook is written    (default: ~/.config/helpme)
#   HELPME_RELEASE_BASE_URL  override asset base URL (e.g. file:///path/to/dist for testing)
set -e

REPO="${HELPME_REPO:-alanmun/helpme}"
VERSION="${HELPME_VERSION:-latest}"
BIN_DIR="${HELPME_BIN_DIR:-$HOME/.local/bin}"
HOOK_DIR="${HELPME_HOOK_DIR:-$HOME/.config/helpme}"

if [ -n "$HELPME_RELEASE_BASE_URL" ]; then
  base="$HELPME_RELEASE_BASE_URL"
elif [ "$VERSION" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi

# Normalize OS.
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux)  os=linux ;;
  darwin) os=darwin ;;
  *) echo "unsupported OS: $os (helpme targets linux/macOS; on Windows use WSL)" >&2; exit 1 ;;
esac

# Normalize arch.
arch=$(uname -m)
case "$arch" in
  x86_64|amd64)  arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

asset="helpme-bin-${os}-${arch}"
mkdir -p "$BIN_DIR" "$HOOK_DIR"

echo "Downloading $asset"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$base/$asset" -o "$BIN_DIR/helpme-bin"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$BIN_DIR/helpme-bin" "$base/$asset"
else
  echo "need curl or wget to download" >&2; exit 1
fi
chmod +x "$BIN_DIR/helpme-bin"

# Emit the shell hook from the binary itself (always version-matched).
shell_name=$(basename "${SHELL:-}")
case "$shell_name" in
  zsh)  rc="$HOME/.zshrc";  hookfile="$HOOK_DIR/helpme.zsh" ;;
  bash) rc="$HOME/.bashrc"; hookfile="$HOOK_DIR/helpme.bash" ;;
  *)
    echo "Binary installed to $BIN_DIR/helpme-bin."
    echo "Unrecognized shell '$shell_name' — write a hook manually, e.g.:"
    echo "  helpme-bin --print-hook zsh > $HOOK_DIR/helpme.zsh && source $HOOK_DIR/helpme.zsh"
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
  *) echo "NOTE: $BIN_DIR is not on your PATH — add it so 'helpme-bin' is found." ;;
esac

echo
echo "Done. Open a new shell (or: source $rc), then run:"
echo "  helpme setup        # pick provider, paste key (hidden), choose model"
echo "Then try:  helpme find -f myfile.txt"
echo "(Prefer env vars? HELPME_API_KEY / ANTHROPIC_API_KEY etc. still work and override.)"
