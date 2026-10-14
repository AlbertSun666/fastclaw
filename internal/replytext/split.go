// Package replytext handles display control markers in assistant replies.
package replytext

import (
	"regexp"
	"strings"
)

var replyTokens = regexp.MustCompile("`+|<\\|split\\|>")
var codeFence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

// Split separates prose messages while preserving markers in Markdown code.
// During streaming, incomplete trailing markers are withheld until resolved.
func Split(text string, streaming bool) []string {
	parts := []string{}
	lines := []string{}
	fence := ""
	inlineCode := ""
	flush := func() {
		if value := strings.TrimSpace(strings.Join(lines, "\n")); value != "" {
			parts = append(parts, value)
		}
		lines = nil
	}
	input := strings.Split(text, "\n")
	for i, line := range input {
		if match := codeFence.FindStringSubmatch(line); len(match) > 0 {
			marker := match[1]
			if fence == "" {
				fence = marker
			} else if marker[0] == fence[0] && len(marker) >= len(fence) {
				fence = ""
			}
			lines = append(lines, line)
			continue
		}
		if fence != "" {
			lines = append(lines, line)
			continue
		}
		start := 0
		for _, loc := range replyTokens.FindAllStringIndex(line, -1) {
			token := line[loc[0]:loc[1]]
			if token[0] == '`' {
				if inlineCode == "" {
					inlineCode = token
				} else if inlineCode == token {
					inlineCode = ""
				}
			} else if inlineCode == "" {
				lines = append(lines, line[start:loc[0]])
				flush()
				start = loc[1]
			}
		}
		tail := line[start:]
		if streaming && inlineCode == "" && i == len(input)-1 {
			// Withhold a delimiter split across deltas, including an inline one.
			const separator = "<|split|>"
			for n := len(separator) - 1; n > 0; n-- {
				if strings.HasSuffix(tail, separator[:n]) {
					tail = tail[:len(tail)-n]
					break
				}
			}
		}
		lines = append(lines, tail)
	}
	flush()
	return parts
}
