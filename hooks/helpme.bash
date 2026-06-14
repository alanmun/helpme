# helpme — bash wrapper.
#
# Usage: helpme <command>
#
# Same behavior as the zsh version: run the command; on success print a green
# confirmation with no LLM call; on failure send the command + its error to the
# helpme binary, explain the fix, and offer the corrected command.
#
# Bash has no equivalent of zsh's `print -z` (push onto the next prompt), so on
# a fix it drops you into an editable, prefilled line — press Enter to run it,
# or edit first.
#
# Note: a `helpme`-prefixed command is executed as typed. Don't `helpme`
# something destructive to "see if it works" — it will run.

helpme() {
  if (( $# == 0 )); then
    echo "usage: helpme <command>   (or: helpme setup)" >&2
    return 1
  fi

  # Meta sub-commands go straight to the binary instead of being run as commands.
  case "$1" in
    setup|--setup)  command helpme-bin setup;     return $? ;;
    --version|-v)   command helpme-bin --version; return $? ;;
  esac

  local errfile
  errfile=$(mktemp) || return 1

  # Run the command, streaming output live while capturing stderr to a file.
  "$@" 2> >(tee "$errfile" >&2)
  local status=$?

  if (( status == 0 )); then
    printf '\033[32m✔ This command already works!\033[0m\n'
    rm -f "$errfile"
    return 0
  fi

  local errtext
  errtext=$(<"$errfile")
  rm -f "$errfile"

  local out
  if ! out=$(command helpme-bin "$@" <<< "$errtext"); then
    printf '\033[31m✘ helpme could not get a fix.\033[0m\n'
    return $status
  fi

  local fixed="${out%%$'\n'*}"   # first line = corrected command
  local why="${out#*$'\n'}"      # remainder  = explanation

  printf '\033[33m» %s\033[0m\n' "$why"

  # Prefill an editable line; Enter runs it.
  local edited
  if IFS= read -r -e -i "$fixed" -p "run> " edited; then
    [ -n "$edited" ] && eval "$edited"
  fi
}
