package site

import (
	"encoding/json"
	"fmt"
	"regexp"
)

func extractJSONScript(markup string, pattern *regexp.Regexp) (map[string]any, error) {
	match := pattern.FindStringSubmatch(markup)
	if len(match) != 2 {
		return nil, fmt.Errorf("embedded JSON script not found")
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(match[1]), &data); err != nil {
		return nil, err
	}
	return data, nil
}

func mapPath(root map[string]any, keys ...string) map[string]any {
	current := root
	for _, key := range keys {
		if current == nil {
			return nil
		}
		next, _ := current[key].(map[string]any)
		current = next
	}
	return current
}
