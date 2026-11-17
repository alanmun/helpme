# helpme — zsh wrapper.
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
# Run mode: runs the command. If it succeeds, prints a green confirmation and
# stops — no LLM call, no tokens spent. If it fails, sends the command plus its
# captured error to the helpme binary, prints a one-line explanation, and pushes
# the corrected command onto the next prompt (via `print -z`) so you can edit it
# or just press Enter.
#
# Ask mode: a single quoted argument containing whitespace is treated as a
# natural-language question/request. It is NEVER executed — helpme asks the AI to
# explain it and, when the request maps to one, suggests a command to prefill.
#
# Note: a `helpme`-prefixed *command* (run mode) is executed as typed. A broken
# command usually just errors harmlessly, but don't `helpme` something
# destructive to "see if it works" — it will run. (A quoted question is safe.)

# The active help conversation lives only in this shell process. It is not
# written to disk, cannot collide with another terminal tab, and disappears
# automatically when the shell exits.
typeset -g __helpme_session_context=""

# Keep the in-memory transcript comfortably below per-process environment limits
# even when a failed command emitted a very large error. Preserve the start
# (original request) and the most recent turns, which carry the useful context.
__helpme_trim_session() {
  emulate -L zsh
  local max=16000
  if (( ${#__helpme_session_context} > max )); then
    __helpme_session_context="${__helpme_session_context[1,5000]}"$'\n\n[... older session detail omitted ...]\n\n'"${__helpme_session_context[-11000,-1]}"
  fi
}

# __helpme_update — re-run the installer, which downloads the latest binary and
# refreshes the hook in place. Override the source with HELPME_REPO/HELPME_INSTALL_BRANCH.
__helpme_update() {
  emulate -L zsh
  local repo="${HELPME_REPO:-alanmun/helpme}"
  local branch="${HELPME_INSTALL_BRANCH:-master}"
  local url="https://raw.githubusercontent.com/$repo/$branch/install.sh"
  print "Updating helpme from $url"
  if (( $+commands[curl] )); then
    curl -fsSL "$url" | sh
  elif (( $+commands[wget] )); then
    wget -qO- "$url" | sh
  else
    print -u2 "helpme: need curl or wget to update"
    return 1
  fi
}

helpme() {
  emulate -L zsh
  if (( $# == 0 )); then
    print -u2 'usage: helpme <command>   (run helpme --help for usage and diagnostics)'
    return 1
  fi

  # Meta flags control helpme itself; everything else is your command or question.
  # They're --flags (not bare words) so they can't shadow a real command.
  case "$1" in
    --help|-h)     HELPME_SHELL=zsh command helpme-bin --help; return $? ;;
    --setup|-s)    command helpme-bin --setup;   return $? ;;
    --update|-u)   __helpme_update;              return $? ;;
    --version|-v)  command helpme-bin --version; return $? ;;
    --continue|-c)
      if [[ -z "${__helpme_session_context-}" ]]; then
        print -u2 'helpme: nothing to continue; start with helpme "your question"'
        return 2
      fi

      shift
      local query="$*"
      local out
      out=$(HELPME_SHELL=zsh HELPME_COLUMNS="${COLUMNS:-}" HELPME_SESSION_CONTEXT="$__helpme_session_context" command helpme-bin --continue "$query")
      local rc=$?
      if (( rc != 0 )); then
        # Keep the prior context unchanged so a transient failure can be retried.
        (( rc == 3 )) || print -P "%F{red}✘ helpme couldn't continue that.%f"
        return $rc
      fi

      local suggested="${out%%$'\n'*}"
      local explanation="${out#*$'\n'}"
      print -r -- $'\033[33m'"${explanation}"$'\033[0m'

      local remembered_query="$query"
      [[ -n "$remembered_query" ]] || remembered_query="(No text supplied; elaborate on the previous answer.)"
      __helpme_session_context+=$'\n\nUser follow-up:\n'"$remembered_query"$'\n\nAssistant answer:\n'"$explanation"
      [[ -n "$suggested" ]] && __helpme_session_context+=$'\nSuggested command: '"$suggested"
      __helpme_trim_session

      # A continuation may refine the original request, so revised commands use
      # the same editable next-prompt behavior as an initial answer.
      [[ -n "$suggested" ]] && print -rz -- "$suggested"
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
    local out
    out=$(HELPME_SHELL=zsh HELPME_COLUMNS="${COLUMNS:-}" command helpme-bin --ask "$1")
    local rc=$?
    if (( rc != 0 )); then
      # rc==3 means "no API key yet": the binary already printed the setup hint.
      (( rc == 3 )) || print -P "%F{red}✘ helpme couldn't answer that.%f"
      return $rc
    fi
    local suggested="${out%%$'\n'*}"   # first line = suggested command (may be empty)
    local explanation="${out#*$'\n'}"  # remainder  = explanation to teach
    # print -r (no -P): the model's text is printed verbatim, so a literal % in
    # the explanation isn't mangled as a prompt escape.
    print -r -- $'\033[33m'"${explanation}"$'\033[0m'
    __helpme_session_context=$'User request:\n'"$1"$'\n\nAssistant answer:\n'"$explanation"
    [[ -n "$suggested" ]] && __helpme_session_context+=$'\nSuggested command: '"$suggested"
    __helpme_trim_session
    [[ -n "$suggested" ]] && print -rz -- "$suggested"
    return 0
  fi

  local errfile
  errfile=$(mktemp) || return 1

  # Run the command, streaming output live while capturing stderr to a file.
  "$@" 2> >(tee "$errfile" >&2)
  local status=$?

  if (( status == 0 )); then
    print -P "%F{green}✔ This command already works!%f"
    command rm -f "$errfile"
    return 0
  fi

  local errtext
  errtext=$(<"$errfile")
  command rm -f "$errfile"

  local attempted="$*"
  local out
  out=$(HELPME_SHELL=zsh command helpme-bin "$@" <<< "$errtext")
  local rc=$?
  if (( rc != 0 )); then
    # rc==3 means "no API key yet": the binary already printed an actionable
    # setup message to stderr, so don't pile the generic failure line on top.
    (( rc == 3 )) || print -P "%F{red}✘ helpme couldn't get a fix.%f"
    return $status
  fi

  local fixed="${out%%$'\n'*}"   # first line = corrected command
  local why="${out#*$'\n'}"      # remainder  = explanation

  print -P "%F{yellow}» ${why}%f"
  __helpme_session_context=$'User asked for help fixing this command:\n'"$attempted"$'\n\nCommand error:\n'"$errtext"$'\n\nAssistant answer:\n'"$why"$'\nSuggested command: '"$fixed"
  __helpme_trim_session
  print -z "$fixed"
}
