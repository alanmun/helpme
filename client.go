// client.go — the LLM round-trips over the OpenAI-style chat API.
//
// Two modes share one POST to {base_url}/chat/completions:
//
//   - fix:  given a failed command + its error, return {cmd, why} to repair it.
//   - ask:  given a natural-language question/request, return {explanation,
//     command} — the command key is optional (some questions don't map to one).
//
// Both use a terse system prompt and a low token cap, so the model returns a
// tight answer instead of an essay. Responses are constrained to JSON and parsed
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

const askPrompt = `You are a command-line expert answering a user from their terminal. They give you a question or a request — often "how do I…" or asking you to build a shell command. Respond with ONLY compact JSON, no markdown, no preamble:
{"explanation":"<your answer>","command":"<single-line shell command>"}
Rules:
- "explanation" is required. "command" is OPTIONAL: include it only when the request maps to a runnable shell command; omit the key entirely when the user just wants an answer.
- When you give a command, make it a single line, ready to run, doing exactly what was asked.
- "explanation" is AT MOST 3 short lines. Use the » character to call out and teach the important parts (flags, arguments) so the user can form the command themselves next time.
- When a well-known mnemonic or memory aid exists, share it (e.g. "tar -xzf » eXtract Ze File", "chmod 755 » rwxr-xr-x").
- Keep "command" on one line. Put nothing outside the JSON.`

// Wire types for the OpenAI-compatible /chat/completions endpoint.
type chatReq struct {
	Model string `json:"model"`
	// Exactly one of these is set: OpenAI's reasoning models require
	// max_completion_tokens; Anthropic-compat and OpenRouter use max_tokens.
	MaxTokens           int           `json:"max_tokens,omitempty"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string        `json:"reasoning_effort,omitempty"`
	Messages            []chatMessage `json:"messages"`
	ResponseFormat      *respFormat   `json:"response_format,omitempty"`
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

// fix is the structured result of fix mode.
type fix struct {
	Cmd string `json:"cmd"`
	Why string `json:"why"`
}

// answer is the structured result of ask mode: an explanation always, plus an
// optional command when the request maps to one.
type answer struct {
	Explanation string `json:"explanation"`
	Command     string `json:"command"`
}

// newChatReq assembles a chat-completions body for a provider, selecting the
// right token field and including reasoning_effort only when set.
func newChatReq(p provider, maxOut int, system, user string) chatReq {
	r := chatReq{
		Model:           p.model,
		ReasoningEffort: p.reasoning, // omitempty drops it when ""
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		ResponseFormat: &respFormat{Type: "json_object"},
	}
	if p.maxCompletionTokens {
		r.MaxCompletionTokens = maxOut
	} else {
		r.MaxTokens = maxOut
	}
	return r
}

func buildRequest(p provider, command, errText string) chatReq {
	// 1024: headroom for reasoning tokens; the JSON answer is tiny.
	return newChatReq(p, 1024, systemPrompt,
		fmt.Sprintf("Command:\n%s\n\nError:\n%s", command, strings.TrimSpace(errText)))
}

func buildAskRequest(p provider, query string) chatReq {
	// 2048: a little more room than fix mode for a 3-line explanation.
	return newChatReq(p, 2048, askPrompt, strings.TrimSpace(query))
}

// chat performs one round-trip and returns the model's message content, or an
// error describing an HTTP/API/decoding failure.
func chat(p provider, req chatReq) (string, error) {
	buf, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(p.baseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		// Anthropic's OpenAI-compat endpoint, OpenAI, and OpenRouter all accept Bearer.
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 300))
	}

	var cr chatResp
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("bad response: %w", err)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("api: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("empty response from model")
	}
	return cr.Choices[0].Message.Content, nil
}

func askFix(p provider, command, errText string) (fix, error) {
	content, err := chat(p, buildRequest(p, command, errText))
	if err != nil {
		return fix{}, err
	}
	return parseFix(content)
}

func askQuestion(p provider, query string) (answer, error) {
	content, err := chat(p, buildAskRequest(p, query))
	if err != nil {
		return answer{}, err
	}
	return parseAnswer(content)
}

// jsonObject pulls the outermost {...} out of the model's text even if it's
// wrapped in code fences or stray prose.
func jsonObject(s string) (string, error) {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end < start {
		return "", fmt.Errorf("no JSON object in model output: %q", truncate(s, 200))
	}
	return s[start : end+1], nil
}

// parseFix extracts the {cmd, why} object and normalizes cmd to one line.
func parseFix(s string) (fix, error) {
	obj, err := jsonObject(s)
	if err != nil {
		return fix{}, err
	}
	var f fix
	if err := json.Unmarshal([]byte(obj), &f); err != nil {
		return fix{}, fmt.Errorf("could not parse model JSON: %w", err)
	}
	f.Cmd = strings.TrimSpace(strings.ReplaceAll(f.Cmd, "\n", " "))
	f.Why = strings.TrimSpace(f.Why)
	if f.Cmd == "" {
		return fix{}, fmt.Errorf("model returned an empty command")
	}
	return f, nil
}

// parseAnswer extracts the {explanation, command} object. command is optional and
// collapsed to one line; explanation is required and capped at 3 lines.
func parseAnswer(s string) (answer, error) {
	obj, err := jsonObject(s)
	if err != nil {
		return answer{}, err
	}
	var a answer
	if err := json.Unmarshal([]byte(obj), &a); err != nil {
		return answer{}, fmt.Errorf("could not parse model JSON: %w", err)
	}
	a.Command = strings.TrimSpace(strings.ReplaceAll(a.Command, "\n", " "))
	a.Explanation = clampLines(strings.TrimSpace(a.Explanation), 3)
	if a.Explanation == "" {
		return answer{}, fmt.Errorf("model returned an empty explanation")
	}
	return a, nil
}

// clampLines keeps at most the first n non-empty lines, guarding against a model
// that ignores the 3-line instruction.
func clampLines(s string, n int) string {
	var kept []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		kept = append(kept, strings.TrimRight(ln, " \t"))
		if len(kept) == n {
			break
		}
	}
	return strings.Join(kept, "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
