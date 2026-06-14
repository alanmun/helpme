# helpme

A terminal helper. Prefix any command with `helpme`:

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

The explanation is deliberately short. This is a fast nudge, not an essay.

## Bring your own AI

helpme talks to any OpenAI-style `/chat/completions` endpoint, so you choose the
provider:

| `HELPME_PROVIDER` | Endpoint | Default model |
|---|---|---|
| `anthropic` (default) | Anthropic OpenAI-compat | `claude-haiku-4-5` |
| `openai` | OpenAI | `gpt-4o-mini` |
| `openrouter` | OpenRouter | `openai/gpt-4o-mini` |
| `custom` | your `HELPME_BASE_URL` | set `HELPME_MODEL` |

```sh
export HELPME_PROVIDER=anthropic
export HELPME_API_KEY=sk-ant-...
# optional:
export HELPME_MODEL=claude-haiku-4-5
```

`custom` points at anything that speaks the same API — Groq, Together, a local
Ollama (`HELPME_BASE_URL=http://localhost:11434/v1`), LM Studio, etc.

Defaults favor the cheap/fast model tier on purpose: a command-fixer should be
snappy.

**Already have a key exported?** If `HELPME_API_KEY` is unset, helpme falls back
to the provider's standard var — `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, or
`OPENROUTER_API_KEY` — so most users need no `HELPME_*` config at all.

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

### Build from source (contributors)

```sh
go build -o ~/.local/bin/helpme-bin .
helpme-bin --print-hook zsh > ~/.config/helpme/helpme.zsh   # or bash
echo 'source ~/.config/helpme/helpme.zsh' >> ~/.zshrc
```

### Cutting a release (maintainers)

```sh
./build-release.sh v0.1.0          # cross-compiles dist/ + SHA256SUMS
gh release create v0.1.0 dist/*    # attach the binaries
```

`install.sh` pulls assets from `github.com/<HELPME_REPO>/releases`; the binaries
are never committed (see `.gitignore`).

## How it's wired

- **`helpme-bin`** (Go, zero dependencies) does only the LLM round-trip: it
  reads the failed command + error and prints two lines — the corrected command,
  then the explanation.
- **The shell wrapper** (`hooks/helpme.zsh` / `hooks/helpme.bash`) owns running
  the command, the success path, and prefilling the fix onto your prompt — the
  part a separate binary can't do, since only the shell can touch its own input
  line.

## A caveat worth knowing

A `helpme`-prefixed command runs as typed. A broken command usually just errors
harmlessly, but don't `helpme` something destructive to "see if it works" — it
will run.
