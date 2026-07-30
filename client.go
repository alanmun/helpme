// client.go — the LLM round-trips over the OpenAI-style chat API.
//
// Three modes share one POST to {base_url}/chat/completions:
//
//   - fix:  given a failed command + its error, return {cmd, why} to repair it.
//   - ask:  given a natural-language question/request, return {explanation,
//     command} — the command key is optional (some questions don't map to one).
//   - continue: given the current shell-local help session and a follow-up,
//     return a more complete {explanation, command}. A revised command is
//     included when the follow-up changes what the command needs to do.
//
// Fix and ask start terse; continuation deliberately has more room to teach.
// Responses are constrained to JSON and parsed defensively (the model
// occasionally wraps it in prose or code fences).
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
Correct the command so it will actually work. Keep "cmd" on one line. "cmd" is the BARE command and nothing else — it gets typed straight onto the user's prompt, so never prefix it with a prompt character or label ($, #, >, », run>) and never wrap it in backticks or a code fence. Put nothing outside the JSON.`

const askPrompt = `You are a command-line expert answering a user typing in their terminal. They give you a question or a request — often "how do I…" or asking you to build a shell command. Respond with ONLY compact JSON, no markdown, no preamble:
{"explanation":"<your answer>","command":"<single-line shell command>"}
Rules:
- "explanation" is REQUIRED. "command" is OPTIONAL: include it ONLY when the request maps to a concrete shell command to run. OMIT the key entirely when the user is just asking a question that doesn't call for a command — never invent one to fill the field.
- When you give a command, make it a single line, ready to run, doing exactly what was asked, and TEACH it: in "explanation" put EACH flag/argument on its OWN line — e.g. "-r recurse into subdirectories", "-n show line numbers", "'help' the search pattern, quoted so the shell leaves it alone", "." the path to search — so the user learns every piece. One short line per flag/argument; only group flags together if there would otherwise be more than a handful of lines.
- When you are only answering a question (no command), give a short plain answer in one to three lines.
- Output PLAIN TEXT lines separated by real newlines. Do NOT add boxes, bullets, »/-> markers, or markdown — the tool formats the lines for display.
- If a well-known mnemonic helps, put it on its own line (e.g. "tar -xzf = eXtract Ze File", "chmod 755 = rwxr-xr-x").
- Keep "command" on one line. It is the BARE command and nothing else — it gets typed straight onto the user's prompt, so never prefix it with a prompt character or label ($, #, >, », run>) and never wrap it in backticks or a code fence.
- Put nothing outside the JSON.`

const continuePrompt = `You are continuing a command-line help conversation. The user is following up because they want to understand the previous answer more deeply or refine what the suggested command should do. Respond with ONLY compact JSON, no markdown, no preamble:
{"explanation":"<your more complete answer>","command":"<single-line shell command>"}
Rules:
- "explanation" is REQUIRED. Give a meaningfully more detailed and complete answer than the initial help response, normally 4 to 10 concise plain-text lines. Explain the reasoning, relevant syntax, and useful caveats instead of merely repeating the prior answer.
- Pay attention to the entire previous session so references such as "that", "it", or "the filename" retain their meaning.
- "command" is OPTIONAL when the follow-up only asks for conceptual explanation.
- "command" is REQUIRED when the follow-up adds, removes, or changes a requirement. Return the complete revised command satisfying BOTH the original request and the new requirement, not merely a fragment or the changed portion.
- When returning a revised command, explain the important changed syntax and how it addresses the follow-up.
- Output PLAIN TEXT lines separated by real newlines. Do NOT add boxes, bullets, »/-> markers, or markdown — the tool formats the lines for display.
- Keep "command" on one line. It is the BARE command and nothing else — it gets typed straight onto the user's prompt, so never prefix it with a prompt character or label ($, #, >, », run>) and never wrap it in backticks or a code fence.
- Put nothing outside the JSON.`

const defaultContinueQuery = "Please elaborate on your previous answer. Give me a more complete explanation, including the reasoning, relevant syntax, and useful caveats."

// maxExplanationLines bounds the explanation — generous enough to teach one line
// per flag/argument plus a mnemonic, tight enough to stay a nudge, not an essay.
const maxExplanationLines = 6

// Follow-ups deliberately have more room: continuing means the user wants to
// learn more than the first zippy answer provided.
const maxContinuationLines = 12

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

func buildContinueRequest(p provider, context, query string) chatReq {
	query = strings.TrimSpace(query)
	if query == "" {
		query = defaultContinueQuery
	}
	user := fmt.Sprintf("Previous help session:\n%s\n\nCurrent follow-up:\n%s",
		strings.TrimSpace(context), query)
	// 4096: follow-ups intentionally explain more and may return a revised command.
	return newChatReq(p, 4096, systemPromptWithEnv(continuePrompt), user)
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

func continueQuestion(p provider, context, query string) (answer, error) {
	content, err := chat(p, buildContinueRequest(p, context, query))
	if err != nil {
		return answer{}, err
	}
	return parseContinuationAnswer(content)
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

// promptMarkers are prefixes a model sometimes glues onto a command even though
// the system prompt asks for a bare one: shell prompts copied out of examples
// ("$ ", "# "), teaching markers ("» "), and run-this labels ("run> "). Longest
// first, so "$>" is peeled before "$".
//
// A bare ">" is deliberately NOT here: "> file" is a real command (truncate),
// and stripping the marker would leave the filename alone on the prompt to be
// executed. Matching is case-insensitive.
var promptMarkers = []string{"shell>", "bash>", "run>", "run:", "sh>", "zsh>", "cmd>", ">>>", "$>", "»", "→", "$", "#", "%"}

// sanitizeCommand reduces a model-supplied command to something safe to prefill:
// one line, no code fence or wrapping backticks, no leading prompt/label marker.
//
// This matters more than it looks. Whatever comes back here is typed verbatim
// onto the user's next prompt line, so a stray label doesn't just look wrong —
// it changes what Enter does. A returned "run> find . -name x" parses in bash as
// "run . -name x > find": command-not-found, plus a stray file named "find", and
// the fix the user asked for never runs.
func sanitizeCommand(s string) string {
	s = strings.TrimSpace(s)

	// Fenced block: the opening fence and its language tag are their own line,
	// so drop that line rather than trying to tell "bash" the tag from "bash"
	// the command.
	if strings.HasPrefix(s, "```") {
		if _, rest, ok := strings.Cut(s, "\n"); ok {
			s = strings.TrimSuffix(strings.TrimSpace(rest), "```")
		} else {
			s = strings.Trim(s, "`")
		}
		s = strings.TrimSpace(s)
	}

	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))

	// `ls -la` -> ls -la
	if len(s) > 1 && strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`") {
		s = strings.TrimSpace(strings.Trim(s, "`"))
	}

	// Peel markers until none match, so stacked decoration ("run> $ ls") goes too.
	for {
		rest, ok := cutPromptMarker(s)
		if !ok {
			return s
		}
		s = rest
	}
}

// cutPromptMarker removes one leading marker from s, reporting whether it found
// one. A marker made only of punctuation must be followed by whitespace, so real
// shell syntax survives: "$(date)" keeps its "$", "#!/bin/sh" keeps its "#".
func cutPromptMarker(s string) (string, bool) {
	lower := strings.ToLower(s)
	for _, m := range promptMarkers {
		rest, ok := strings.CutPrefix(lower, m)
		if !ok {
			continue
		}
		// Labels ending in > or : are unambiguous; bare punctuation needs a space.
		if !strings.HasSuffix(m, ">") && !strings.HasSuffix(m, ":") &&
			(rest == "" || !isSpace(rest[0])) {
			continue
		}
		// Cut from s, not lower, to preserve the command's original case.
		return strings.TrimSpace(s[len(m):]), true
	}
	return s, false
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t'
}

// parseFix extracts the {cmd, why} object and sanitizes cmd into a bare,
// single-line command safe to prefill.
func parseFix(s string) (fix, error) {
	obj, err := jsonObject(s)
	if err != nil {
		return fix{}, err
	}
	var f fix
	if err := json.Unmarshal([]byte(obj), &f); err != nil {
		return fix{}, fmt.Errorf("could not parse model JSON: %w; got %q", err, truncate(obj, 200))
	}
	f.Cmd = sanitizeCommand(f.Cmd)
	// One line, per the 15-word contract: the wrapper prints why as a single
	// "» ..." line, so any extra line the model tacks on (a repeat of the command,
	// a "run> ..." label) would land in the terminal raw, looking like output the
	// user is meant to act on.
	f.Why = clampLines(strings.TrimSpace(f.Why), 1)
	if f.Cmd == "" {
		return fix{}, fmt.Errorf("model returned an empty command")
	}
	return f, nil
}

// parseAnswer extracts the {explanation, command} object. command is optional and
// sanitized the same way as a fix; explanation is required and kept zippy.
func parseAnswer(s string) (answer, error) {
	return parseAnswerLines(s, maxExplanationLines)
}

// parseContinuationAnswer uses the same wire shape, but preserves the additional
// detail requested by continuation mode.
func parseContinuationAnswer(s string) (answer, error) {
	return parseAnswerLines(s, maxContinuationLines)
}

func parseAnswerLines(s string, maxLines int) (answer, error) {
	obj, err := jsonObject(s)
	if err != nil {
		return answer{}, err
	}
	var a answer
	if err := json.Unmarshal([]byte(obj), &a); err != nil {
		return answer{}, fmt.Errorf("could not parse model JSON: %w; got %q", err, truncate(obj, 200))
	}
	a.Command = sanitizeCommand(a.Command)
	a.Explanation = clampLines(strings.TrimSpace(a.Explanation), maxLines)
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
