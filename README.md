# helpme

Ever started typing a command into your shell and realize you forgot how to use it? With AI, shouldn't there a be a fast way to learn the command and run it?

```bash
alanmun@ubuntumachine:~/Repositories/my-codebase$ helpme git reset to undo a single commit I made so I can commit it to a diff branch
fatal: ambiguous argument 'to': unknown revision or path not in the working tree.
Use '--' to separate paths from revisions, like this:
'git <command> [<revision>...] -- [<file>...]'
» Use git reset HEAD~1 to undo the last commit and keep your changes.
alanmun@ubuntumachine:~/Repositories/my-codebase$ git reset HEAD~1
Unstaged changes after reset:
M       handler.py
D       install-vscode-extensions.sh
M       products/investments.py

```

### 1. Fix a command — prefix it with `helpme`

```
helpme find -f Volumes.lua
```

- **If the command already works,** it just runs — and you get a green
  `✔ This command already works!`. No AI call, no tokens, no waiting.
- **If it fails,** helpme sends the command *and its actual error* to your LLM,
  prints a quick explanation of what went wrong/what you meant to do, and drops the corrected
  command onto your next prompt to edit or run.

```
$ helpme find -f Volumes.lua
find: unknown predicate `-f'
» -f isn't a find flag; use -name to match by filename
$ find . -name Volumes.lua        ← prefilled, edit or press Enter
```

### 2. Ask a question — put it in quotes

Give `helpme` a single **quoted** argument and it answers in plain language
instead of running anything. Ask it to build a command, or just ask a question:

```
$ helpme "search all of this dir: ./flerbo/flop for instances of 'bert' or 'blart'"
» grep -r recurses; -E enables the | alternation
» quote the pattern so the shell doesn't expand it
$ grep -rE 'bert|blart' ./flerbo/flop   ← prefilled when a command fits
```

When the request maps to a shell command, helpme suggests one and prefills it
onto your next prompt — exactly like the fix flow. When it's just a question, it
only explains. Either single or double quotes work, so you can nest the other
kind inside:

```
helpme 'how do I rename files so any instance of "stealth" becomes "ninja"?'
```

A quoted question is **never executed** — only your AI sees it. (helpme treats a
single argument that contains spaces as a question; an unquoted command like
`helpme find -f x` still runs.)

The explanation is deliberately short — a `»` one-liner for a simple answer, or a
tidy box that breaks down each flag/argument (with a mnemonic when one exists).
A fast nudge, not an essay:

```
$ helpme "search this folder for 'help' by file and line"
┌────────────────────────────────────────────────────────────────┐
│ -r recurse into subdirectories                                 │
│ -n show line numbers                                           │
│ 'help' the search pattern, quoted so the shell leaves it alone │
│ . the path to search                                           │
└────────────────────────────────────────────────────────────────┘
$ grep -rn 'help' .                ← prefilled, edit or press Enter
```

## BYOAI (Bring your own AI)

helpme talks to any OpenAI-style `/chat/completions` endpoint, so you choose the
provider:

| Provider | Endpoint | Default model |
|---|---|---|
| `anthropic` (default) | Anthropic OpenAI-compat | `claude-sonnet-4-6` |
| `openai` | OpenAI | `gpt-5.4-mini` |
| `openrouter` | OpenRouter | `anthropic/claude-sonnet-4.6` |
| `custom` | your base URL | (set a model) |

**Easiest — run the wizard** (no env vars to babysit):

```sh
helpme --setup      # or: helpme -s
```

It prompts for provider, model, and key. The key is read with echo off (it
never appears on screen, like a password prompt), and a masked confirmation
(`got: sk-abc…`) is printed so you can still tell the right key landed. It's
then saved to `~/.config/helpme/config.json` (mode `0600`). That's it — no
`export` lines in your shell rc.

`custom` points at anything that speaks the same API — Groq, Together, a local
Ollama (`http://localhost:11434/v1`), LM Studio, etc.

Defaults run at **low reasoning** for speed — a command-fixer should be snappy.
That's sent as `reasoning_effort: low` for OpenAI/OpenRouter; for Anthropic it's
omitted because the compat endpoint already runs without extended thinking (=
low reasoning) and may 400 on the field. Override with `HELPME_REASONING`
(`low`/`medium`/`high`/`minimal`/`off`) or the `reasoning` config key.

**Prefer env vars / CI?** They still work and **override the config file**:
`HELPME_PROVIDER`, `HELPME_API_KEY`, `HELPME_MODEL`, `HELPME_BASE_URL`,
`HELPME_TIMEOUT` (request timeout in seconds, default `30`). And if
`HELPME_API_KEY` is unset, helpme falls back to the provider's standard var
(`ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `OPENROUTER_API_KEY`). Full precedence
per setting: **env var > config file > built-in default**.

> helpme only reads **API keys**. It deliberately does **not** use the OAuth
> tokens from Claude Code or Codex sign-in: those are tied to consumer plans
> (Pro/Max/Plus) whose entitlement isn't licensed to third-party apps, and
> borrowing them risks getting the account banned.

## Install

No Go toolchain needed — the installer downloads a prebuilt static binary:

```sh
curl -fsSL https://raw.githubusercontent.com/alanmun/helpme/master/install.sh | sh
```

It detects your OS/arch, drops `helpme-bin` into `~/.local/bin`, writes the
shell hook (emitted from the binary, so it always matches), and adds one
`source` line to your `~/.zshrc` or `~/.bashrc`. Open a new shell, set your
provider env vars, done. Linux and macOS (Intel + Apple Silicon).

**Windows:** run the same one-liner from **MSYS2**, **Git Bash**, or **Cygwin**
— these are POSIX bash on top of Windows, so the installer fetches the native
`helpme-bin.exe` and wires up the bash hook exactly as on Linux (works in the
mintty terminal these ship with). Plain **PowerShell/cmd** aren't supported yet
— use one of the above, or **WSL** (which installs as regular Linux).

### Updating

Re-running the installer **is** the update — it downloads the latest binary and
refreshes the hook in place (and won't nag you to set up again if you already
have a config). From inside helpme:

```sh
helpme --update      # or: helpme -u
```

### Local testing / build from source

No release needed — build and wire up the hook in one step (requires Go).
**Source** it to also load `helpme` into your current shell (no new shell, no
manual `source` afterward):

```sh
source ./dev-install.sh
```

Running it as `./dev-install.sh` still builds and installs, but a child process
can't change your shell, so you'd then open a new shell or `source ~/.zshrc`.

Or do it by hand:

```sh
go build -o ~/.local/bin/helpme-bin .
helpme-bin --print-hook zsh > ~/.config/helpme/helpme.zsh   # or bash
echo 'source ~/.config/helpme/helpme.zsh' >> ~/.zshrc
```

### Cutting a release (maintainers)

One command — builds, tags, pushes the tag, and publishes the GitHub release
with the binaries attached (needs Go and an authenticated `gh`):

```sh
./release.sh v0.1.0
```

It cross-compiles `dist/` + `SHA256SUMS`, tags `v0.1.0` at HEAD, pushes it, and
runs `gh release create` with the assets and auto-generated notes. Idempotent —
re-run the same version to retry a half-finished release (it reuses the tag and
clobbers the uploaded assets). Refuses to run on a dirty tree or off `master`
(override with `HELPME_ALLOW_DIRTY=1` / `HELPME_RELEASE_BRANCH`); a
`vX.Y.Z-rc1` tag is published as a pre-release so it never becomes `latest`.

Need just the binaries (no publish)? `./build-release.sh v0.1.0` still works on
its own. `install.sh` pulls assets from `github.com/<HELPME_REPO>/releases`; the
binaries are never committed (see `.gitignore`).

## How it's wired

- **`helpme-bin`** (Go, no third-party dependencies) does the LLM round-trips and
  `helpme --setup`. In fix mode it reads the failed command + error; in ask mode
  (`--ask "<question>"`) it reads a plain-language request. Both print two parts —
  a suggested command on line 1 (empty when there isn't one), then the
  explanation — so the wrapper parses them the same way.
- **The shell wrapper** (`hooks/helpme.zsh` / `hooks/helpme.bash`) decides which
  mode to use (a single quoted argument → ask), owns running the command, the
  success path, and prefilling the suggestion onto your prompt — the part a
  separate binary can't do, since only the shell can touch its own input line.

## Troubleshooting

helpme keeps **no logs by default** — your commands and prompts never touch
disk. When something misbehaves, turn logging on for a run:

```sh
HELPME_DEBUG=1 helpme <command>            # full request/response to stderr
HELPME_LOG=~/.config/helpme/helpme.log helpme <command>   # append to a file
```

The log shows the exact endpoint, request body, HTTP status, timing, and the
raw model response (the `Authorization` header / your key is never logged).
That distinguishes the common failures:

- `empty response body (HTTP 200 …)` — the provider returned nothing; usually a
  timeout or rate limit. Bump `HELPME_TIMEOUT` (seconds) for slow/reasoning
  models.
- `could not parse model JSON … got "…"` — the model wrapped its JSON in prose;
  the raw text it returned is shown so you can see what happened.
- `api 4xx/5xx: …` — the provider rejected the request (bad key, model name, or
  an unsupported field); the provider's own message is included.

## A caveat worth knowing

A `helpme`-prefixed command runs as typed. A broken command usually just errors
harmlessly, but don't `helpme` something destructive to "see if it works" — it
will run.
