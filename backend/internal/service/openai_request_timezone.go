package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

var openAIRequestTimezoneTagPattern = regexp.MustCompile(`(?s)(<environment_context\b[^>]*>.*?<timezone>).*?(</timezone>.*?</environment_context>)`)

// NormalizeOpenAIRequestTimezone validates the optional timezone override used
// for OpenAI request metadata. Empty disables the override; Local is rejected
// because it depends on the gateway process rather than an explicit IANA zone.
func NormalizeOpenAIRequestTimezone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if value == "Local" {
		return "", fmt.Errorf("timezone Local is not allowed; use an IANA timezone")
	}
	if _, err := time.LoadLocation(value); err != nil {
		return "", fmt.Errorf("invalid IANA timezone %q", value)
	}
	return value, nil
}

// RewriteOpenAIRequestTimezone changes only timezone metadata already present
// in an OpenAI JSON request. It never creates a timezone field. The XML form
// is the Codex environment context; the structured form is the web-search
// user_location metadata. If no supported field is found, the original bytes
// are returned unchanged.
func RewriteOpenAIRequestTimezone(body []byte, target string) ([]byte, bool, error) {
	target, err := NormalizeOpenAIRequestTimezone(target)
	if err != nil {
		return nil, false, err
	}
	if target == "" || len(bytes.TrimSpace(body)) == 0 {
		return body, false, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return body, false, nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return body, false, nil
	}
	changed := rewriteOpenAIRequestTimezoneValue(&value, nil, target)
	if !changed {
		return body, false, nil
	}
	rewritten, err := json.Marshal(value)
	if err != nil {
		return nil, false, err
	}
	return rewritten, true, nil
}

func (s *OpenAIGatewayService) rewriteOpenAIRequestTimezonePayload(ctx context.Context, body []byte) ([]byte, error) {
	if s == nil || s.settingService == nil {
		return body, nil
	}
	if policy, enabled := s.codexRequestStrategyPolicyForScope(ctx, CodexRequestStrategyScopeDedicated); enabled {
		rewritten, _, err := ApplyCodexRequestBodyPolicy(body, policy)
		return rewritten, err
	}
	target := s.settingService.GetOpenAIRequestTimezone(ctx)
	if target == "" {
		return body, nil
	}
	rewritten, _, err := RewriteOpenAIRequestTimezone(body, target)
	if err != nil {
		return nil, err
	}
	return rewritten, nil
}

func rewriteOpenAIRequestTimezoneValue(value *any, path []string, target string) bool {
	switch typed := (*value).(type) {
	case string:
		if !isOpenAIRequestTimezoneTextPath(path) {
			return false
		}
		rewritten := openAIRequestTimezoneTagPattern.ReplaceAllString(typed, "${1}"+target+"${2}")
		if rewritten == typed {
			return false
		}
		*value = rewritten
		return true
	case []any:
		changed := false
		for i := range typed {
			item := any(typed[i])
			if rewriteOpenAIRequestTimezoneValue(&item, path, target) {
				typed[i] = item
				changed = true
			}
		}
		return changed
	case map[string]any:
		changed := false
		for key, item := range typed {
			if key == "timezone" && isOpenAIRequestTimezoneMetadataPath(path) {
				if current, ok := item.(string); ok && current != target {
					typed[key] = target
					changed = true
					continue
				}
			}
			nextPath := append(append([]string(nil), path...), key)
			if rewriteOpenAIRequestTimezoneValue(&item, nextPath, target) {
				typed[key] = item
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func isOpenAIRequestTimezoneTextPath(path []string) bool {
	if len(path) == 0 {
		return false
	}
	switch path[0] {
	case "input", "messages", "instructions", "prompt":
		return true
	default:
		return false
	}
}

func isOpenAIRequestTimezoneMetadataPath(path []string) bool {
	if len(path) == 0 {
		return true
	}
	parent := path[len(path)-1]
	if parent == "user_location" {
		return len(path) == 1 || (len(path) == 2 && path[0] == "tools")
	}
	if parent == "session" {
		return len(path) == 1
	}
	return parent == "metadata" && len(path) == 1
}
