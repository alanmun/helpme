#!/usr/bin/env sh
# dev-install.sh — build from source and wire up the shell hook, for local
# testing. No GitHub release required (unlike install.sh, which downloads).
#
#   source ./dev-install.sh    build, install, AND load helpme into THIS shell
#   ./dev-install.sh           build + install; then open a new shell (or source
#                              your rc) to load it
#
# A child process can't change its parent's shell, so to get `helpme` working in
# your CURRENT shell — no new shell, no manual source — you must *source* this
# script. When sourced it loads the hook (and puts helpme-bin on PATH) for you.
#
# Requires Go on PATH. If you used the repo's local Go install, first run:
#   export PATH="$HOME/.local/go/bin:$PATH"

# Are we being sourced? If so we can load the hook into the caller's shell, and
# we must never `exit` (that would kill their shell) — only `return`.
_helpme_sourced=0
if [ -n "${BASH_VERSION:-}" ]; then
  [ "${BASH_SOURCE}" != "$0" ] && _helpme_sourced=1
elif [ -n "${ZSH_VERSION:-}" ]; then
  case "${ZSH_EVAL_CONTEXT:-}" in *:file*) _helpme_sourced=1 ;; esac
fi

# Path to this script — BASH_SOURCE under bash, $0 under zsh/sh (both correct when
# sourced). Used to locate the repo so `go build` runs in the right place.
if [ -n "${BASH_SOURCE:-}" ]; then _helpme_self="${BASH_SOURCE}"; else _helpme_self="$0"; fi

# The real work lives in a function so failures can `return` a code without an
# `exit` reaching a sourcing shell. No `set -e`: that would leak errexit into the
# caller; instead the few critical steps are checked explicitly.
_helpme_dev_install() {
  _helpme_hookfile=""
  repo_dir=$(CDPATH= cd -- "$(dirname -- "$1")" && pwd) || return 1
  _helpme_bindir="${HELPME_BIN_DIR:-$HOME/.local/bin}"
  hook_dir="${HELPME_HOOK_DIR:-$HOME/.config/helpme}"
  mkdir -p "$_helpme_bindir" "$hook_dir" || return 1

  # On native Windows (MSYS2 / Git Bash / Cygwin) the binary needs a .exe suffix.
  bin_ext=""
  case "$(uname -s)" in MSYS*|MINGW*|CYGWIN*) bin_ext=".exe" ;; esac
  bin="$_helpme_bindir/helpme-bin$bin_ext"

  if ! command -v go >/dev/null 2>&1; then
    echo "Go not found on PATH." >&2
    echo "If you used the repo's local Go install, run this first:" >&2
    echo '  export PATH="$HOME/.local/go/bin:$PATH"' >&2
    echo "Otherwise install Go: https://go.dev/dl/" >&2
    return 1
  fi

  echo "Building helpme-bin -> $bin"
  ( cd "$repo_dir" && go build -o "$bin" . ) \
    || { echo "helpme: build failed" >&2; return 1; }

  # Wire up the running shell when sourced (most accurate), else fall back to $SHELL.
  if [ -n "${ZSH_VERSION:-}" ]; then shell_name=zsh
  elif [ -n "${BASH_VERSION:-}" ]; then shell_name=bash
  else shell_name=$(basename "${SHELL:-}"); fi

  case "$shell_name" in
    zsh)  rc="$HOME/.zshrc";  hookfile="$hook_dir/helpme.zsh" ;;
    bash) rc="$HOME/.bashrc"; hookfile="$hook_dir/helpme.bash" ;;
    *)
      echo "Built. Source a hook from hooks/ manually for shell '$shell_name'."
      return 0
      ;;
  esac

  "$bin" --print-hook "$shell_name" > "$hookfile" || return 1

  line="source \"$hookfile\""
  if ! grep -qsF "$line" "$rc"; then
    printf '\n# helpme\n%s\n' "$line" >> "$rc"
    echo "Added helpme hook to $rc"
  else
    echo "helpme hook already present in $rc"
  fi

  _helpme_hookfile="$hookfile"
  return 0
}

_helpme_dev_install "$_helpme_self"
_helpme_rc=$?

if [ "$_helpme_sourced" = 1 ]; then
  # Load helpme straight into this shell.
  if [ "$_helpme_rc" = 0 ] && [ -n "${_helpme_hookfile:-}" ] && [ -f "${_helpme_hookfile}" ]; then
    case ":$PATH:" in
      *":$_helpme_bindir:"*) ;;
      *) PATH="$_helpme_bindir:$PATH"; export PATH
         echo "Added $_helpme_bindir to PATH for this shell." ;;
    esac
    . "${_helpme_hookfile}"
    echo
    echo "✔ helpme is loaded in this shell. Try:  helpme --setup"
  fi
else
  if [ "$_helpme_rc" = 0 ]; then
    case ":$PATH:" in
      *":${_helpme_bindir:-}:"*) ;;
      *) echo "NOTE: ${_helpme_bindir} is not on PATH — add it (e.g. export PATH=\"${_helpme_bindir}:\$PATH\")." ;;
    esac
    echo
    echo "Done. To load helpme now without opening a new shell, run:"
    echo "  source ./dev-install.sh"
    echo "Or open a new shell. Then:  helpme --setup"
  fi
fi

# Tidy up so a sourced run leaves the caller's namespace clean.
unset -f _helpme_dev_install 2>/dev/null
unset _helpme_self _helpme_hookfile _helpme_bindir repo_dir hook_dir rc hookfile line shell_name bin bin_ext 2>/dev/null

if [ "$_helpme_sourced" = 1 ]; then
  _helpme_sourced=0
  unset _helpme_sourced
  return "${_helpme_rc:-0}"
fi
unset _helpme_sourced
exit "${_helpme_rc:-0}"
