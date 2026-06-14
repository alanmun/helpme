# helpme — bash wrapper.
#
# Usage: helpme <command>
#
# Same behavior as the zsh version: run the command; on success print a green
# confirmation with no LLM call; on failure send the command + its error to the
# helpme binary, explain the fix, and offer the corrected command.
#
# On a fix, the corrected command is pushed onto your NEXT prompt (editable,
# runs as a normal command, lands in history) using the readline trick below.
#
# Note: a `helpme`-prefixed command is executed as typed. Don't `helpme`
# something destructive to "see if it works" — it will run.

# __helpme_prefill — bash's equivalent of zsh `print -z`: put text on the next
# prompt's command line. We bind the terminal's status-report reply (ESC [ 0 n)
# to insert the text, then ask the terminal to send that reply (ESC [ 5 n). The
# reply is buffered until the next prompt's readline reads it, firing the macro.
__helpme_prefill() {
  local text="$1"
  text="${text//\\/\\\\}"   # escape backslashes for the readline macro
  text="${text//\"/\\\"}"   # escape double quotes
  bind "\"\\e[0n\": \"$text\"" 2>/dev/null
  printf '\e[5n'
}

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
  __helpme_prefill "$fixed"
}
