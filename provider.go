// provider.go — resolves which LLM endpoint to call from the environment.
//
// "Bring your own AI": one OpenAI-style /chat/completions client targets any
// provider by swapping base_url + key + model. Anthropic, OpenAI, and
// OpenRouter all speak that shape (Anthropic via its OpenAI-compatible
// endpoint), and "custom" lets you point at anything else that does too —
// Groq, Together, a local Ollama/LM Studio server, etc.
//
// Resolution precedence per setting is: env var > saved config (helpme setup) >
// built-in default. This lets `helpme setup` be the no-fuss path while env vars
// still override for power users and CI.
//
//	HELPME_PROVIDER  anthropic | openai | openrouter | custom   (default: anthropic)
//	HELPME_API_KEY   the provider key (optional for local "custom" servers)
//	HELPME_MODEL     override the default model for the chosen provider
//	HELPME_BASE_URL  required for "custom"; also overrides any provider's URL
//	HELPME_REASONING low | medium | high | minimal | off       (reasoning effort)
//
// API key fallback chain: HELPME_API_KEY > config file > the provider's standard
// var (ANTHROPIC_API_KEY / OPENAI_API_KEY / OPENROUTER_API_KEY). Only API keys
// are read; helpme never touches subscription OAuth tokens (Claude Code / Codex
// sign-in), whose consumer-plan entitlement isn't licensed to third-party apps.
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// errNoAPIKey is the one "expected" misconfiguration: the user hasn't supplied a
// key yet. main() detects it (errors.Is) and exits with a distinct code so the
// shell wrapper shows this actionable message alone, without its generic
// "couldn't get a fix" line piled on top.
var errNoAPIKey = errors.New("no API key found")

type provider struct {
	baseURL string
	apiKey  string
	model   string
	// reasoning effort to request (low|medium|high|minimal); "" omits the field.
	reasoning string
	// OpenAI's reasoning models reject max_tokens and want max_completion_tokens.
	maxCompletionTokens bool
	// timeout bounds the whole round-trip (connect + body read). Reasoning models
	// can be slow, so it's generous by default and tunable via HELPME_TIMEOUT.
	timeout time.Duration
}

// defaultTimeout is intentionally roomy: a too-tight deadline fires mid-body-read
// and surfaces as a baffling "empty/short response" rather than a clear timeout.
const defaultTimeout = 30 * time.Second

// Defaults: a capable-but-fast model at low reasoning — smart enough to fix a
// command, never slow enough to write an essay.
//
// reasoning is sent as the OpenAI-standard `reasoning_effort`. It's left empty
// for "anthropic": its OpenAI-compat endpoint already runs without extended
// thinking by default (= low reasoning), so sending the field is redundant and
// risks a 400 on a layer that may not map it. OpenAI/OpenRouter take it directly.
var providerDefaults = map[string]struct {
	baseURL, model, reasoning string
	maxCompletionTokens       bool
}{
	"anthropic":  {"https://api.anthropic.com/v1", "claude-sonnet-4-6", "", false},
	"openai":     {"https://api.openai.com/v1", "gpt-5.4-mini", "low", true},
	"openrouter": {"https://openrouter.ai/api/v1", "anthropic/claude-sonnet-4.6", "low", false},
}

// Standard key env var per provider, used as a fallback for HELPME_API_KEY.
var providerKeyEnv = map[string]string{
	"anthropic":  "ANTHROPIC_API_KEY",
	"openai":     "OPENAI_API_KEY",
	"openrouter": "OPENROUTER_API_KEY",
}

// pick returns the first non-empty of env var, config value, then default.
func pick(envKey, cfgVal, def string) string {
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v
	}
	if v := strings.TrimSpace(cfgVal); v != "" {
		return v
	}
	return def
}

func loadProvider() (provider, error) {
	cfg := readConfig()

	name := strings.ToLower(pick("HELPME_PROVIDER", cfg.Provider, "anthropic"))

	d, known := providerDefaults[name]
	if !known && name != "custom" {
		return provider{}, fmt.Errorf("unknown provider %q (use anthropic|openai|openrouter|custom)", name)
	}

	p := provider{
		baseURL:             pick("HELPME_BASE_URL", cfg.BaseURL, d.baseURL),
		model:               pick("HELPME_MODEL", cfg.Model, d.model),
		maxCompletionTokens: d.maxCompletionTokens,
	}

	// Reasoning effort: HELPME_REASONING > config > provider default; off/none omit it.
	reasoning := strings.ToLower(pick("HELPME_REASONING", cfg.Reasoning, d.reasoning))
	switch reasoning {
	case "off", "none", "disabled":
		reasoning = ""
	}
	p.reasoning = reasoning

	// Timeout (seconds) via HELPME_TIMEOUT; ignore junk/non-positive values.
	p.timeout = defaultTimeout
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("HELPME_TIMEOUT"))); err == nil && n > 0 {
		p.timeout = time.Duration(n) * time.Second
	}

	// API key: HELPME_API_KEY > config > provider-standard env var.
	p.apiKey = pick("HELPME_API_KEY", cfg.APIKey, "")
	if p.apiKey == "" {
		if ev := providerKeyEnv[name]; ev != "" {
			p.apiKey = strings.TrimSpace(os.Getenv(ev))
		}
	}

	if p.baseURL == "" {
		return p, fmt.Errorf("no base URL for provider %q; run 'helpme --setup' or set HELPME_BASE_URL", name)
	}
	if p.model == "" {
		return p, fmt.Errorf("no model selected; run 'helpme --setup' or set HELPME_MODEL")
	}
	if p.apiKey == "" && name != "custom" {
		hint := "run 'helpme --setup', or set HELPME_API_KEY"
		if ev := providerKeyEnv[name]; ev != "" {
			hint += " / " + ev
		}
		return p, fmt.Errorf("%w; %s", errNoAPIKey, hint)
	}
	return p, nil
}
