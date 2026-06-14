// provider.go — resolves which LLM endpoint to call from the environment.
//
// "Bring your own AI": one OpenAI-style /chat/completions client targets any
// provider by swapping base_url + key + model. Anthropic, OpenAI, and
// OpenRouter all speak that shape (Anthropic via its OpenAI-compatible
// endpoint), and "custom" lets you point at anything else that does too —
// Groq, Together, a local Ollama/LM Studio server, etc.
//
// Env vars:
//
//	HELPME_PROVIDER  anthropic | openai | openrouter | custom   (default: anthropic)
//	HELPME_API_KEY   the provider key (optional for local "custom" servers)
//	HELPME_MODEL     override the default model for the chosen provider
//	HELPME_BASE_URL  required for "custom"; also overrides any provider's URL
package main

import (
	"fmt"
	"os"
	"strings"
)

type provider struct {
	baseURL string
	apiKey  string
	model   string
}

// Sensible fast/cheap defaults — this tool wants a snappy one-liner, never an essay.
var providerDefaults = map[string]struct{ baseURL, model string }{
	"anthropic":  {"https://api.anthropic.com/v1", "claude-haiku-4-5"},
	"openai":     {"https://api.openai.com/v1", "gpt-4o-mini"},
	"openrouter": {"https://openrouter.ai/api/v1", "openai/gpt-4o-mini"},
}

func loadProvider() (provider, error) {
	name := strings.ToLower(strings.TrimSpace(os.Getenv("HELPME_PROVIDER")))
	if name == "" {
		name = "anthropic"
	}

	var p provider
	if d, ok := providerDefaults[name]; ok {
		p.baseURL, p.model = d.baseURL, d.model
	} else if name == "custom" {
		p.baseURL = os.Getenv("HELPME_BASE_URL")
		if p.baseURL == "" {
			return p, fmt.Errorf("HELPME_PROVIDER=custom requires HELPME_BASE_URL")
		}
	} else {
		return p, fmt.Errorf("unknown HELPME_PROVIDER %q (use anthropic|openai|openrouter|custom)", name)
	}

	if m := strings.TrimSpace(os.Getenv("HELPME_MODEL")); m != "" {
		p.model = m
	}
	// Allow a base-URL override for any provider (e.g. a corporate proxy).
	if b := strings.TrimSpace(os.Getenv("HELPME_BASE_URL")); b != "" {
		p.baseURL = b
	}

	p.apiKey = strings.TrimSpace(os.Getenv("HELPME_API_KEY"))
	if p.apiKey == "" && name != "custom" {
		return p, fmt.Errorf("HELPME_API_KEY is not set")
	}
	if p.model == "" {
		return p, fmt.Errorf("no model selected; set HELPME_MODEL")
	}
	return p, nil
}
