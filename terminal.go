package main

import (
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultTerminalColumns = 80
	maxTerminalColumns     = 1000
	boxFrameColumns        = 4 // "│ " + content + " │"
	minBoxColumns          = 6 // enough room for one two-cell character
)

// renderExplanation formats the model's plain-text explanation for the terminal.
// The shell wrapper passes its live COLUMNS value because stdout is captured by
// command substitution and is therefore not a TTY here.
func renderExplanation(s string) string {
	return renderExplanationAtWidth(s, explanationColumns())
}

func renderExplanationAtWidth(s string, columns int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= 1 {
		return "» " + s
	}

	columns = normalizeColumns(columns)
	if columns < minBoxColumns {
		return strings.Join(wrapDisplayLines(lines, columns), "\n")
	}

	lines = wrapDisplayLines(lines, columns-boxFrameColumns)
	contentWidth := 0
	for _, line := range lines {
		if width := displayWidth(line); width > contentWidth {
			contentWidth = width
		}
	}

	rule := strings.Repeat("─", contentWidth+2)
	var b strings.Builder
	b.WriteString("┌" + rule + "┐\n")
	for _, line := range lines {
		padding := strings.Repeat(" ", contentWidth-displayWidth(line))
		b.WriteString("│ " + line + padding + " │\n")
	}
	b.WriteString("└" + rule + "┘")
	return b.String()
}

func explanationColumns() int {
	for _, name := range []string{"HELPME_COLUMNS", "COLUMNS"} {
		columns, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
		if err == nil && columns > 0 {
			return normalizeColumns(columns)
		}
	}
	return defaultTerminalColumns
}

func normalizeColumns(columns int) int {
	if columns < 1 {
		return defaultTerminalColumns
	}
	if columns > maxTerminalColumns {
		return maxTerminalColumns
	}
	return columns
}

func wrapDisplayLines(lines []string, width int) []string {
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, wrapDisplayLine(expandTabs(line, 4), width)...)
	}
	return wrapped
}

// wrapDisplayLine prefers whitespace boundaries and hard-wraps only words that
// cannot fit. It measures terminal cells, not bytes or Unicode code points.
func wrapDisplayLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}

	var wrapped []string
	for displayWidth(line) > width {
		cells := 0
		position := 0
		lastSpaceStart := -1
		lastSpaceEnd := -1

		for position < len(line) {
			size, clusterWidth, isSpace := nextDisplayCluster(line[position:])
			if cells+clusterWidth > width {
				break
			}
			if isSpace {
				lastSpaceStart = position
				lastSpaceEnd = position + size
			}
			cells += clusterWidth
			position += size
		}

		if lastSpaceStart > 0 {
			wrapped = append(wrapped, strings.TrimRightFunc(line[:lastSpaceStart], unicode.IsSpace))
			line = strings.TrimLeftFunc(line[lastSpaceEnd:], unicode.IsSpace)
			continue
		}

		// width is at least two for boxed output, so every display cluster fits.
		// The unboxed narrow-terminal path has more room than this content limit.
		if position == 0 {
			size, _, _ := nextDisplayCluster(line)
			position = size
		}
		wrapped = append(wrapped, line[:position])
		line = line[position:]
	}

	wrapped = append(wrapped, line)
	return wrapped
}

func expandTabs(s string, tabWidth int) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}

	var b strings.Builder
	column := 0
	for _, r := range s {
		if r == '\t' {
			spaces := tabWidth - column%tabWidth
			b.WriteString(strings.Repeat(" ", spaces))
			column += spaces
			continue
		}
		b.WriteRune(r)
		column += runeCellWidth(r)
	}
	return b.String()
}

func displayWidth(s string) int {
	width := 0
	for len(s) > 0 {
		size, clusterWidth, _ := nextDisplayCluster(s)
		width += clusterWidth
		s = s[size:]
	}
	return width
}

// nextDisplayCluster handles the terminal-width cases that rune counts miss:
// combining marks, emoji modifiers, flags, and zero-width-joiner emoji.
func nextDisplayCluster(s string) (size, width int, isSpace bool) {
	r, runeSize := utf8.DecodeRuneInString(s)
	if runeSize == 0 {
		return 0, 0, false
	}

	size = runeSize
	width = runeCellWidth(r)
	isSpace = unicode.IsSpace(r)

	if isRegionalIndicator(r) {
		if next, nextSize := utf8.DecodeRuneInString(s[size:]); isRegionalIndicator(next) {
			size += nextSize
		}
		return size, 2, false
	}

	for size < len(s) {
		next, nextSize := utf8.DecodeRuneInString(s[size:])
		if next == '\u200d' {
			size += nextSize
			if size >= len(s) {
				break
			}
			joined, joinedSize := utf8.DecodeRuneInString(s[size:])
			size += joinedSize
			if joinedWidth := runeCellWidth(joined); joinedWidth > width {
				width = joinedWidth
			}
			continue
		}
		if !isClusterExtension(next) {
			break
		}
		size += nextSize
		if next == '\ufe0f' || next == '\u20e3' {
			width = 2
		}
	}
	return size, width, isSpace
}

func isClusterExtension(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me, unicode.Cf) ||
		(r >= 0x1f3fb && r <= 0x1f3ff) ||
		(r >= 0xe0020 && r <= 0xe007f)
}

func runeCellWidth(r rune) int {
	if r == 0 || r < 0x20 || (r >= 0x7f && r < 0xa0) || isClusterExtension(r) {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isRegionalIndicator(r rune) bool {
	return r >= 0x1f1e6 && r <= 0x1f1ff
}

// These ranges cover East Asian wide/full-width characters and the emoji that
// modern terminals render as two cells. Ambiguous-width characters stay at one.
func isWideRune(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f ||
		r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) ||
		(r >= 0x20000 && r <= 0x3fffd))
}
