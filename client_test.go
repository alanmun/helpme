package main

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestBuildAskRequest(t *testing.T) {
	// Ask mode reuses the same token/reasoning plumbing as fix mode.
	openai := string(mustJSON(buildAskRequest(provider{model: "gpt-5.4-mini", reasoning: "low", maxCompletionTokens: true}, "how do I find big files?")))
	if !strings.Contains(openai, `"max_completion_tokens"`) || strings.Contains(openai, `"max_tokens"`) {
		t.Fatalf("openai ask should use max_completion_tokens only: %s", openai)
	}
	if !strings.Contains(openai, `"reasoning_effort":"low"`) {
		t.Fatalf("openai ask should send reasoning_effort: %s", openai)
	}
	// It must use the ask system prompt (asks for "explanation"), not the fix one.
	if !strings.Contains(openai, "explanation") {
		t.Fatalf("ask request should carry the ask system prompt: %s", openai)
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
		// Regression: chatty model appends prose (containing a 'z' and a later
		// brace). First-brace-to-last-brace used to sweep it in and fail with
		// "invalid character 'z' after top-level value".
		{"trailing prose with brace", `{"cmd":"tar -xf a.tar","why":"plain tar, drop -z"} note: zellij is {neat}`, "tar -xf a.tar", false},
		{"brace inside string value", `{"cmd":"echo {x}","why":"braces ok"}`, "echo {x}", false},
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

func TestParseAnswer(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantCmd  string
		wantExpl string
		wantErr  bool
	}{
		{"with command", `{"explanation":"» -name matches by filename","command":"find . -name '*.log'"}`, "find . -name '*.log'", "» -name matches by filename", false},
		{"no command key", `{"explanation":"Use Ctrl-R to search history"}`, "", "Use Ctrl-R to search history", false},
		{"empty command", `{"explanation":"just an answer","command":""}`, "", "just an answer", false},
		{"code fence", "```json\n{\"explanation\":\"x\",\"command\":\"ls\"}\n```", "ls", "x", false},
		{"command newlines collapse", "{\"explanation\":\"x\",\"command\":\"echo \\na\"}", "echo  a", "x", false},
		{"no json", `I cannot help`, "", "", true},
		{"empty explanation", `{"explanation":"","command":"ls"}`, "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseAnswer(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Command != c.wantCmd {
				t.Fatalf("command = %q, want %q", got.Command, c.wantCmd)
			}
			if got.Explanation != c.wantExpl {
				t.Fatalf("explanation = %q, want %q", got.Explanation, c.wantExpl)
			}
		})
	}
}

func TestRenderExplanation(t *testing.T) {
	// A single line gets the » teaching prefix, no box.
	if got := renderExplanation("Use Ctrl-R to search history"); got != "» Use Ctrl-R to search history" {
		t.Fatalf("single line = %q, want a » prefix", got)
	}

	// A multi-line explanation is boxed: top border, one row per line, bottom border.
	got := renderExplanation("-r recurse\n-n line numbers")
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 lines (top, 2 content, bottom), got %d:\n%s", len(lines), got)
	}
	if !strings.HasPrefix(lines[0], "┌") || !strings.HasSuffix(lines[0], "┐") {
		t.Fatalf("top border malformed: %q", lines[0])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "└") || !strings.HasSuffix(lines[len(lines)-1], "┘") {
		t.Fatalf("bottom border malformed: %q", lines[len(lines)-1])
	}
	// Content must appear inside the box.
	if !strings.Contains(got, "-r recurse") || !strings.Contains(got, "-n line numbers") {
		t.Fatalf("box dropped content:\n%s", got)
	}
	// Every row is the same display width — the right border lines up.
	w := utf8.RuneCountInString(lines[0])
	for i, ln := range lines {
		if rw := utf8.RuneCountInString(ln); rw != w {
			t.Fatalf("row %d width %d != %d: %q", i, rw, w, ln)
		}
	}
	// Widest content is "-n line numbers" (15 runes); box = 15 + "│ "/" │" + borders = 19.
	if w != 19 {
		t.Fatalf("box width = %d, want 19", w)
	}
}

func TestClampLines(t *testing.T) {
	// More than 3 non-empty lines are trimmed to the first 3; blanks are dropped.
	got := clampLines("one\n\ntwo\nthree\nfour", 3)
	if got != "one\ntwo\nthree" {
		t.Fatalf("clampLines = %q, want %q", got, "one\ntwo\nthree")
	}
	// Fewer than the cap is returned intact.
	if got := clampLines("only one", 3); got != "only one" {
		t.Fatalf("clampLines = %q, want %q", got, "only one")
	}
}
