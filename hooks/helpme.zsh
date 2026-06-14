# helpme — zsh wrapper.
#
# Usage: helpme <command>
#
# Runs the command. If it succeeds, prints a green confirmation and stops — no
# LLM call, no tokens spent. If it fails, sends the command plus its captured
# error to the helpme binary, prints a one-line explanation, and pushes the
# corrected command onto the next prompt (via `print -z`) so you can edit it or
# just press Enter.
#
# Note: a `helpme`-prefixed command is executed as typed. A broken command
# usually just errors harmlessly, but don't `helpme` something destructive to
# "see if it works" — it will run.

helpme() {
  emulate -L zsh
  if (( $# == 0 )); then
    print -u2 "usage: helpme <command>"
    return 1
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

  local out
  if ! out=$(command helpme-bin "$@" <<< "$errtext"); then
    print -P "%F{red}✘ helpme couldn't get a fix.%f"
    return $status
  fi

  local fixed="${out%%$'\n'*}"   # first line = corrected command
  local why="${out#*$'\n'}"      # remainder  = explanation

  print -P "%F{yellow}» ${why}%f"
  print -z "$fixed"
}
