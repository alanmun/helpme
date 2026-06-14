// client.go — the single LLM round-trip over the OpenAI-style chat API.
//
// One POST to {base_url}/chat/completions with a terse system prompt and a low
// token cap, so the model returns a one-line fix and a one-breath explanation
// instead of an essay. The response is constrained to JSON and parsed
// defensively (the model occasionally wraps it in prose or code fences).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const systemPrompt = `You fix broken shell commands. You are given the command the user ran and the error it produced. Respond with ONLY compact JSON, no markdown, no preamble:
{"cmd":"<corrected command, single line, ready to run>","why":"<15 words max, plain language, teach the fix>"}
Correct the command so it will actually work. Keep "cmd" on one line. Put nothing outside the JSON.`

// Wire types for the OpenAI-compatible /chat/completions endpoint.
type chatReq struct {
	Model          string        `json:"model"`
	MaxTokens      int           `json:"max_tokens"`
	Messages       []chatMessage `json:"messages"`
	ResponseFormat *respFormat   `json:"response_format,omitempty"`
}

type respFormat struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// fix is the structured result the model returns.
type fix struct {
	Cmd string `json:"cmd"`
	Why string `json:"why"`
}

func askFix(p provider, command, errText string) (fix, error) {
	reqBody := chatReq{
		Model:     p.model,
		MaxTokens: 150,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: fmt.Sprintf("Command:\n%s\n\nError:\n%s", command, strings.TrimSpace(errText))},
		},
		ResponseFormat: &respFormat{Type: "json_object"},
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return fix{}, err
	}

	url := strings.TrimRight(p.baseURL, "/") + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return fix{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		// Anthropic's OpenAI-compat endpoint, OpenAI, and OpenRouter all accept Bearer.
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return fix{}, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fix{}, fmt.Errorf("api %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 300))
	}

	var cr chatResp
	if err := json.Unmarshal(raw, &cr); err != nil {
		return fix{}, fmt.Errorf("bad response: %w", err)
	}
	if cr.Error != nil {
		return fix{}, fmt.Errorf("api: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return fix{}, fmt.Errorf("empty response from model")
	}
	return parseFix(cr.Choices[0].Message.Content)
}

// parseFix pulls the {cmd, why} object out of the model's text even if it's
// wrapped in code fences or stray prose, then normalizes cmd to one line.
func parseFix(s string) (fix, error) {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end < start {
		return fix{}, fmt.Errorf("no JSON object in model output: %q", truncate(s, 200))
	}
	var f fix
	if err := json.Unmarshal([]byte(s[start:end+1]), &f); err != nil {
		return fix{}, fmt.Errorf("could not parse model JSON: %w", err)
	}
	f.Cmd = strings.TrimSpace(strings.ReplaceAll(f.Cmd, "\n", " "))
	f.Why = strings.TrimSpace(f.Why)
	if f.Cmd == "" {
		return fix{}, fmt.Errorf("model returned an empty command")
	}
	return f, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
