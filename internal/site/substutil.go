package site

import (
	"encoding/json"
	"strings"
)

func mustLoadSubstMap(raw string) map[string]string {
	result := make(map[string]string)
	_ = json.Unmarshal([]byte(raw), &result)
	return result
}

func applySubstMap(text string, mapping map[string]string) string {
	if len(mapping) == 0 || text == "" {
		return text
	}
	var b strings.Builder
	for _, r := range text {
		if repl, ok := mapping[string(r)]; ok {
			b.WriteString(repl)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isAnyMarkerContained(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
