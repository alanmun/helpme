# helpme

A terminal helper with two modes.

### 1. Fix a command — prefix it with `helpme`

```
helpme find -f Volumes.lua
```

- **If the command already works,** it just runs — and you get a green
  `✔ This command already works!`. No AI call, no tokens, no waiting.
- **If it fails,** helpme sends the command *and its actual error* to an AI,
  prints a one-line explanation of what went wrong, and drops the corrected
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

The explanation is deliberately short — at most three lines, with `»` marking the
parts worth learning (and a mnemonic when one exists). This is a fast nudge, not
an essay.

## Bring your own AI

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
helpme setup
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
`HELPME_PROVIDER`, `HELPME_API_KEY`, `HELPME_MODEL`, `HELPME_BASE_URL`. And if
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
curl -fsSL https://raw.githubusercontent.com/alanmun/helpme/main/install.sh | sh
```

It detects your OS/arch, drops `helpme-bin` into `~/.local/bin`, writes the
shell hook (emitted from the binary, so it always matches), and adds one
`source` line to your `~/.zshrc` or `~/.bashrc`. Open a new shell, set your
provider env vars, done. Linux and macOS (Intel + Apple Silicon); on Windows,
use WSL.

### Local testing / build from source

No release needed — build and wire up the hook in one step (requires Go):

```sh
./dev-install.sh
```

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
  `helpme setup`. In fix mode it reads the failed command + error; in ask mode
  (`--ask "<question>"`) it reads a plain-language request. Both print two parts —
  a suggested command on line 1 (empty when there isn't one), then the
  explanation — so the wrapper parses them the same way.
- **The shell wrapper** (`hooks/helpme.zsh` / `hooks/helpme.bash`) decides which
  mode to use (a single quoted argument → ask), owns running the command, the
  success path, and prefilling the suggestion onto your prompt — the part a
  separate binary can't do, since only the shell can touch its own input line.

## A caveat worth knowing

A `helpme`-prefixed command runs as typed. A broken command usually just errors
harmlessly, but don't `helpme` something destructive to "see if it works" — it
will run.
