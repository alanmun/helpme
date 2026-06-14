// config.go — persistent config so users don't have to keep env vars in their
// shell rc. `helpme setup` writes ~/.config/helpme/config.json (mode 0600,
// since it holds an API key); loadProvider reads it as the middle layer of the
// env > config > default precedence chain.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	Provider  string `json:"provider,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
	Model     string `json:"model,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

// configPath honors XDG_CONFIG_HOME, falling back to ~/.config.
func configPath() string {
	dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "helpme", "config.json")
}

// readConfig returns an empty config if the file is missing or unreadable —
// helpme falls back to env vars and defaults in that case.
func readConfig() config {
	var c config
	b, err := os.ReadFile(configPath())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	return c
}

func writeConfig(c config) error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600) // 0600: holds an API key
}

// runSetup is the interactive `helpme setup` wizard.
func runSetup() {
	in := bufio.NewReader(os.Stdin)
	cur := readConfig()

	fmt.Println("helpme setup")
	fmt.Println("Config will be saved to:", configPath())
	fmt.Println()

	provider := strings.ToLower(prompt(in, "Provider (anthropic/openai/openrouter/custom)", firstNonEmpty(cur.Provider, "anthropic")))
	if _, known := providerDefaults[provider]; !known && provider != "custom" {
		fmt.Fprintf(os.Stderr, "helpme: unknown provider %q\n", provider)
		os.Exit(2)
	}

	baseURL := cur.BaseURL
	if provider == "custom" {
		baseURL = prompt(in, "Base URL (OpenAI-compatible, e.g. http://localhost:11434/v1)", cur.BaseURL)
		if baseURL == "" {
			fmt.Fprintln(os.Stderr, "helpme: custom provider needs a base URL")
			os.Exit(2)
		}
	} else {
		baseURL = "" // use the built-in default for known providers
	}

	model := prompt(in, "Model", firstNonEmpty(cur.Model, providerDefaults[provider].model))

	key := promptKey(in)
	if key == "" {
		key = cur.APIKey
	}

	// Reasoning isn't prompted (defaults to low per provider); preserve any
	// value already set in the file rather than wiping it.
	c := config{Provider: provider, APIKey: key, Model: model, BaseURL: baseURL, Reasoning: cur.Reasoning}
	if err := writeConfig(c); err != nil {
		fmt.Fprintln(os.Stderr, "helpme: could not save config:", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("Saved  provider=%s  model=%s  key=%s\n", c.Provider, c.Model, maskKey(c.APIKey))
	fmt.Println("Try:  helpme find -f myfile.txt")
}

func prompt(in *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// promptKey reads the API key with normal (visible) echo — deliberately NOT
// hidden input. This is a local, one-time setup and "did my paste actually
// land?" matters more than hiding the key from your own screen. After capture
// it prints a masked confirmation.
func promptKey(in *bufio.Reader) string {
	fmt.Print("API key (blank to keep existing): ")
	line, _ := in.ReadString('\n')
	key := strings.TrimSpace(line)
	if key != "" {
		fmt.Println("  got:", maskKey(key))
	}
	return key
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// maskKey shows the first few characters then asterisks — enough to confirm the
// right key landed without printing the whole secret again.
func maskKey(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return "(none)"
	}
	shown := 6
	if len(k) < shown {
		shown = len(k)
	}
	return k[:shown] + "**********"
}
