package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildRequest(t *testing.T) {
	// OpenAI reasoning model: max_completion_tokens + reasoning_effort, no max_tokens.
	openai := string(mustJSON(buildRequest(provider{model: "gpt-5.4-mini", reasoning: "low", maxCompletionTokens: true}, "ls", "boom")))
	if !strings.Contains(openai, `"max_completion_tokens"`) || strings.Contains(openai, `"max_tokens"`) {
		t.Fatalf("openai should use max_completion_tokens only: %s", openai)
	}
	if !strings.Contains(openai, `"reasoning_effort":"low"`) {
		t.Fatalf("openai should send reasoning_effort: %s", openai)
	}

	// Anthropic-style: max_tokens, reasoning_effort omitted when empty.
	anthropic := string(mustJSON(buildRequest(provider{model: "claude-sonnet-4-6", reasoning: "", maxCompletionTokens: false}, "ls", "boom")))
	if !strings.Contains(anthropic, `"max_tokens"`) || strings.Contains(anthropic, `"max_completion_tokens"`) {
		t.Fatalf("anthropic should use max_tokens only: %s", anthropic)
	}
	if strings.Contains(anthropic, `"reasoning_effort"`) {
		t.Fatalf("anthropic should omit reasoning_effort when empty: %s", anthropic)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestParseFix(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantCmd string
		wantErr bool
	}{
		{"plain json", `{"cmd":"find . -name x","why":"use -name"}`, "find . -name x", false},
		{"code fence", "```json\n{\"cmd\":\"ls -la\",\"why\":\"long form\"}\n```", "ls -la", false},
		{"prose wrapped", `Sure! {"cmd":"grep -r foo .","why":"recurse"} hope that helps`, "grep -r foo .", false},
		{"newline in cmd collapses", "{\"cmd\":\"echo \\na\",\"why\":\"x\"}", "echo  a", false},
		{"no json", `I cannot help with that`, "", true},
		{"empty cmd", `{"cmd":"","why":"nothing"}`, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseFix(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got cmd=%q", got.Cmd)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Cmd != c.wantCmd {
				t.Fatalf("cmd = %q, want %q", got.Cmd, c.wantCmd)
			}
		})
	}
}
