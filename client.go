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
	"unicode/utf8"
)

const systemPrompt = `You fix broken shell commands. You are given the command the user ran and the error it produced. Respond with ONLY compact JSON, no markdown, no preamble:
{"cmd":"<corrected command, single line, ready to run>","why":"<15 words max, plain language, teach the fix>"}
Correct the command so it will actually work. Keep "cmd" on one line. Put nothing outside the JSON.`

const askPrompt = `You are a command-line expert answering a user typing in their terminal. They give you a question or a request — often "how do I…" or asking you to build a shell command. Respond with ONLY compact JSON, no markdown, no preamble:
{"explanation":"<your answer>","command":"<single-line shell command>"}
Rules:
- "explanation" is REQUIRED. "command" is OPTIONAL: include it ONLY when the request maps to a concrete shell command to run. OMIT the key entirely when the user is just asking a question that doesn't call for a command — never invent one to fill the field.
- When you give a command, make it a single line, ready to run, doing exactly what was asked, and TEACH it: in "explanation" put EACH flag/argument on its OWN line — e.g. "-r recurse into subdirectories", "-n show line numbers", "'help' the search pattern, quoted so the shell leaves it alone", "." the path to search — so the user learns every piece. One short line per flag/argument; only group flags together if there would otherwise be more than a handful of lines.
- When you are only answering a question (no command), give a short plain answer in one to three lines.
- Output PLAIN TEXT lines separated by real newlines. Do NOT add boxes, bullets, »/-> markers, or markdown — the tool formats the lines for display.
- If a well-known mnemonic helps, put it on its own line (e.g. "tar -xzf = eXtract Ze File", "chmod 755 = rwxr-xr-x").
- Keep "command" on one line. Put nothing outside the JSON.`

// maxExplanationLines bounds the explanation — generous enough to teach one line
// per flag/argument plus a mnemonic, tight enough to stay a nudge, not an essay.
const maxExplanationLines = 6

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
	return newChatReq(p, 1024, systemPromptWithEnv(systemPrompt),
		fmt.Sprintf("Command:\n%s\n\nError:\n%s", command, strings.TrimSpace(errText)))
}

func buildAskRequest(p provider, query string) chatReq {
	// 2048: a little more room than fix mode for a 3-line explanation.
	return newChatReq(p, 2048, systemPromptWithEnv(askPrompt), strings.TrimSpace(query))
}

// chat performs one round-trip and returns the model's message content, or an
// error describing an HTTP/API/decoding failure.
func chat(p provider, req chatReq) (string, error) {
	buf, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(p.baseURL, "/") + "/chat/completions"
	debugf("POST %s  model=%s  timeout=%s", url, p.model, p.timeout)
	debugf("request body: %s", buf)

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		// Anthropic's OpenAI-compat endpoint, OpenAI, and OpenRouter all accept Bearer.
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	start := time.Now()
	resp, err := (&http.Client{Timeout: p.timeout}).Do(httpReq)
	if err != nil {
		debugf("transport error after %s: %v", since(start), err)
		return "", fmt.Errorf("request failed after %s: %w", since(start), err)
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(resp.Body)
	dur := since(start)
	debugf("HTTP %d in %s, %d bytes", resp.StatusCode, dur, len(raw))
	debugf("response body: %s", raw)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 300))
	}
	// A read error here (e.g. the client timeout firing mid-body) used to be
	// swallowed and resurface downstream as a confusing "unexpected end of JSON
	// input". Name it instead.
	if readErr != nil {
		return "", fmt.Errorf("reading response body (HTTP 200, %d bytes in %s): %w", len(raw), dur, readErr)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", fmt.Errorf("empty response body (HTTP 200 in %s) — provider returned nothing; could be a timeout, rate limit, or unsupported request. Re-run with HELPME_DEBUG=1 for the full exchange", dur)
	}

	var cr chatResp
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("bad response envelope (HTTP 200, %d bytes): %w; body=%q", len(raw), err, truncate(strings.TrimSpace(string(raw)), 200))
	}
	if cr.Error != nil {
		return "", fmt.Errorf("api: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("no choices in response: %s", truncate(strings.TrimSpace(string(raw)), 200))
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

// jsonObject pulls the FIRST complete {...} object out of the model's text, even
// when it's wrapped in code fences or trailed by prose. It scans from the first
// '{' to its matching '}', tracking string literals and escapes so braces inside
// values don't confuse it. (A naive first-'{'-to-last-'}' span breaks when the
// model adds chatty text after the JSON — e.g. "...} note: zellij {x}" parsed as
// trailing garbage: "invalid character 'z' after top-level value".)
func jsonObject(s string) (string, error) {
	start := strings.IndexByte(s, '{')
	if start == -1 {
		return "", fmt.Errorf("no JSON object in model output: %q", truncate(strings.TrimSpace(s), 200))
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return s[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("unterminated JSON object in model output: %q", truncate(strings.TrimSpace(s), 200))
}

// parseFix extracts the {cmd, why} object and normalizes cmd to one line.
func parseFix(s string) (fix, error) {
	obj, err := jsonObject(s)
	if err != nil {
		return fix{}, err
	}
	var f fix
	if err := json.Unmarshal([]byte(obj), &f); err != nil {
		return fix{}, fmt.Errorf("could not parse model JSON: %w; got %q", err, truncate(obj, 200))
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
		return answer{}, fmt.Errorf("could not parse model JSON: %w; got %q", err, truncate(obj, 200))
	}
	a.Command = strings.TrimSpace(strings.ReplaceAll(a.Command, "\n", " "))
	a.Explanation = clampLines(strings.TrimSpace(a.Explanation), maxExplanationLines)
	if a.Explanation == "" {
		return answer{}, fmt.Errorf("model returned an empty explanation")
	}
	return a, nil
}

// renderExplanation formats the model's plain-text explanation for the terminal:
// a single line gets a » teaching prefix; a multi-line explanation is wrapped in
// a box so the per-argument breakdown reads as one tidy block. The model never
// draws this itself — it just returns the lines — so the look is consistent.
func renderExplanation(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= 1 {
		return "» " + s
	}

	width := 0
	for _, ln := range lines {
		if w := utf8.RuneCountInString(ln); w > width {
			width = w
		}
	}

	rule := strings.Repeat("─", width+2)
	var b strings.Builder
	b.WriteString("┌" + rule + "┐\n")
	for _, ln := range lines {
		pad := strings.Repeat(" ", width-utf8.RuneCountInString(ln))
		b.WriteString("│ " + ln + pad + " │\n")
	}
	b.WriteString("└" + rule + "┘")
	return b.String()
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
