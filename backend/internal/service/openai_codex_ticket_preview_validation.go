package service

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/http/httpguts"
)

var codexTicketPreviewHeaders = map[string]bool{
	"user-agent": true, "originator": true, "version": true,
	"x-codex-routing-hint": true, "x-codex-beta-features": true,
	"openai-beta": true, "accept-encoding": true,
}

func validateCodexTicketPreviewRules(input CodexTicketRequestPreviewInput) []CodexTicketPreviewValidationError {
	result := []CodexTicketPreviewValidationError{}
	add := func(field, message string) {
		result = append(result, CodexTicketPreviewValidationError{Field: field, Message: message})
	}
	if len(input.Set)+len(input.Remove) > 32 {
		add("rules", "At most 32 header rules are allowed")
		return result
	}
	seen := map[string]bool{}
	total := 0
	checkName := func(name, field string) bool {
		key := strings.ToLower(name)
		if !httpguts.ValidHeaderFieldName(name) || !codexTicketPreviewHeaders[key] {
			add(field, "Header is not an allowed experimental header")
			return false
		}
		if seen[key] {
			add(field, "Duplicate header or conflicting set/remove rule")
			return false
		}
		seen[key] = true
		return true
	}
	for i, rule := range input.Set {
		field := fmt.Sprintf("set[%d]", i)
		checkName(rule.Name, field+".name")
		total += len(rule.Name) + len(rule.Value)
		if len(rule.Value) > 2048 || !utf8.ValidString(rule.Value) {
			add(field+".value", "Header value must be valid UTF-8 and at most 2048 bytes")
			continue
		}
		for _, c := range rule.Value {
			if c < 32 || c == 127 {
				add(field+".value", "Header value contains a forbidden control character")
				break
			}
		}
	}
	for i, name := range input.Remove {
		checkName(name, fmt.Sprintf("remove[%d]", i))
		total += len(name)
	}
	if total > 16<<10 {
		add("rules", "Header rules exceed 16384 bytes")
	}
	return result
}
