# helpme — bash wrapper.
#
# Usage:
#   helpme <command>        run a command; fix it if it fails
#   helpme "a question"     ask in plain language (a single quoted argument)
#   helpme --setup | -s     run the config wizard
#   helpme --update | -u    update to the latest release
#   helpme --version | -v   print the version
#
# Run mode: run the command; on success print a green confirmation with no LLM
# call; on failure send the command + its error to the helpme binary, explain the
# fix, and offer the corrected command.
#
# Ask mode: a single quoted argument containing whitespace is treated as a
# natural-language question/request. It is NEVER executed — helpme just asks the
# AI to explain it and, when the request maps to one, suggests a command.
#
# In both modes the suggested command is pushed onto your NEXT prompt (editable,
# runs as a normal command, lands in history) using the readline trick below.
#
# Note: a `helpme`-prefixed *command* (run mode) is executed as typed. Don't
# `helpme` something destructive to "see if it works" — it will run. (A quoted
# question is safe; it never runs.)

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

# __helpme_update — re-run the installer, which downloads the latest binary and
# refreshes the hook in place. Override the source with HELPME_REPO/HELPME_INSTALL_BRANCH.
__helpme_update() {
  local repo="${HELPME_REPO:-alanmun/helpme}"
  local branch="${HELPME_INSTALL_BRANCH:-master}"
  local url="https://raw.githubusercontent.com/$repo/$branch/install.sh"
  echo "Updating helpme from $url"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" | sh
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "$url" | sh
  else
    echo "helpme: need curl or wget to update" >&2
    return 1
  fi
}

helpme() {
  if (( $# == 0 )); then
    echo 'usage: helpme <command>   (or: helpme "a question", helpme --setup, helpme --update)' >&2
    return 1
  fi

  # Meta flags control helpme itself; everything else is your command or question.
  # They're --flags (not bare words) so they can't shadow a real command.
  case "$1" in
    --setup|-s)    command helpme-bin --setup;   return $? ;;
    --update|-u)   __helpme_update;              return $? ;;
    --version|-v)  command helpme-bin --version; return $? ;;
  esac

  # Ask mode: a single quoted argument containing whitespace is a plain-language
  # question, not a command. We never run it — just ask the AI to explain and (if
  # it maps to one) suggest a command to prefill.
  if (( $# == 1 )) && [[ "$1" == *[[:space:]]* ]]; then
    local out rc
    out=$(HELPME_SHELL=bash command helpme-bin --ask "$1"); rc=$?
    if (( rc != 0 )); then
      # rc==3 means "no API key yet": the binary already printed the setup hint.
      (( rc == 3 )) || printf '\033[31m✘ helpme could not answer that.\033[0m\n'
      return $rc
    fi
    local suggested="${out%%$'\n'*}"   # first line = suggested command (may be empty)
    local explanation="${out#*$'\n'}"  # remainder  = explanation to teach
    # %s keeps the model's text out of the format string, so a stray % is safe.
    printf '\033[33m%s\033[0m\n' "$explanation"
    [[ -n "$suggested" ]] && __helpme_prefill "$suggested"
    return 0
  fi

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

  local out rc
  out=$(HELPME_SHELL=bash command helpme-bin "$@" <<< "$errtext"); rc=$?
  if (( rc != 0 )); then
    # rc==3 means "no API key yet": the binary already printed an actionable
    # setup message to stderr, so don't pile the generic failure line on top.
    (( rc == 3 )) || printf '\033[31m✘ helpme could not get a fix.\033[0m\n'
    return $status
  fi

  local fixed="${out%%$'\n'*}"   # first line = corrected command
  local why="${out#*$'\n'}"      # remainder  = explanation

  printf '\033[33m» %s\033[0m\n' "$why"
  __helpme_prefill "$fixed"
}
