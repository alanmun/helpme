# helpme — bash wrapper.
#
# Usage:
#   helpme <command>        run a command; fix it if it fails
#   helpme "a question"     ask in plain language (a single quoted argument)
#   helpme -c [follow-up]   continue the current help session (blank = elaborate)
#   helpme --help | -h      show usage and local connection diagnostics
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

# The active help conversation lives only in this shell process. It is not
# written to disk, cannot collide with another terminal tab, and disappears
# automatically when the shell exits.
__helpme_session_context=""

# Keep the in-memory transcript comfortably below per-process environment limits
# even when a failed command emitted a very large error. Preserve the start
# (original request) and the most recent turns, which carry the useful context.
__helpme_trim_session() {
  local max=16000
  if (( ${#__helpme_session_context} > max )); then
    __helpme_session_context="${__helpme_session_context:0:5000}"$'\n\n[... older session detail omitted ...]\n\n'"${__helpme_session_context: -11000}"
  fi
}

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
    echo 'usage: helpme <command>   (run helpme --help for usage and diagnostics)' >&2
    return 1
  fi

  # Meta flags control helpme itself; everything else is your command or question.
  # They're --flags (not bare words) so they can't shadow a real command.
  case "$1" in
    --help|-h)     HELPME_SHELL=bash command helpme-bin --help; return $? ;;
    --setup|-s)    command helpme-bin --setup;   return $? ;;
    --update|-u)   __helpme_update;              return $? ;;
    --version|-v)  command helpme-bin --version; return $? ;;
    --continue|-c)
      if [[ -z "${__helpme_session_context-}" ]]; then
        echo 'helpme: nothing to continue; start with helpme "your question"' >&2
        return 2
      fi

      shift
      local query="$*"
      local out rc
      out=$(HELPME_SHELL=bash HELPME_COLUMNS="${COLUMNS:-}" HELPME_SESSION_CONTEXT="$__helpme_session_context" command helpme-bin --continue "$query"); rc=$?
      if (( rc != 0 )); then
        # Keep the prior context unchanged so a transient failure can be retried.
        (( rc == 3 )) || printf '\033[31m✘ helpme could not continue that.\033[0m\n'
        return $rc
      fi

      local suggested="${out%%$'\n'*}"
      local explanation="${out#*$'\n'}"
      printf '\033[33m%s\033[0m\n' "$explanation"

      local remembered_query="$query"
      [[ -n "$remembered_query" ]] || remembered_query="(No text supplied; elaborate on the previous answer.)"
      __helpme_session_context+=$'\n\nUser follow-up:\n'"$remembered_query"$'\n\nAssistant answer:\n'"$explanation"
      [[ -n "$suggested" ]] && __helpme_session_context+=$'\nSuggested command: '"$suggested"
      __helpme_trim_session

      # A continuation may refine the original request, so revised commands use
      # the same editable next-prompt behavior as an initial answer.
      [[ -n "$suggested" ]] && __helpme_prefill "$suggested"
      return 0
      ;;
  esac

  # Every ordinary help request starts a new session, even if the command works
  # or the AI request fails. Only -c/--continue retains the previous context.
  __helpme_session_context=""

  # Ask mode: a single quoted argument containing whitespace is a plain-language
  # question, not a command. We never run it — just ask the AI to explain and (if
  # it maps to one) suggest a command to prefill.
  if (( $# == 1 )) && [[ "$1" == *[[:space:]]* ]]; then
    local out rc
    out=$(HELPME_SHELL=bash HELPME_COLUMNS="${COLUMNS:-}" command helpme-bin --ask "$1"); rc=$?
    if (( rc != 0 )); then
      # rc==3 means "no API key yet": the binary already printed the setup hint.
      (( rc == 3 )) || printf '\033[31m✘ helpme could not answer that.\033[0m\n'
      return $rc
    fi
    local suggested="${out%%$'\n'*}"   # first line = suggested command (may be empty)
    local explanation="${out#*$'\n'}"  # remainder  = explanation to teach
    # %s keeps the model's text out of the format string, so a stray % is safe.
    printf '\033[33m%s\033[0m\n' "$explanation"
    __helpme_session_context=$'User request:\n'"$1"$'\n\nAssistant answer:\n'"$explanation"
    [[ -n "$suggested" ]] && __helpme_session_context+=$'\nSuggested command: '"$suggested"
    __helpme_trim_session
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

  local attempted="$*"
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
  __helpme_session_context=$'User asked for help fixing this command:\n'"$attempted"$'\n\nCommand error:\n'"$errtext"$'\n\nAssistant answer:\n'"$why"$'\nSuggested command: '"$fixed"
  __helpme_trim_session
  __helpme_prefill "$fixed"
}
