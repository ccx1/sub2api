package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// codexRequestStrategyPolicyForScope loads the single policy used by HTTP and
// WebSocket transports. A missing or invalid setting is fail-open for the
// request path; the admin endpoint still reports the configuration error.
func (s *OpenAIGatewayService) codexRequestStrategyPolicyForScope(ctx context.Context, scope string) (CodexRequestStrategyPolicy, bool) {
	if s == nil || s.settingService == nil {
		return CodexRequestStrategyPolicy{}, false
	}
	policy, err := s.settingService.GetCodexRequestStrategyPolicy(ctx)
	if err != nil || !policy.Enabled {
		return policy, false
	}
	if scope == "" {
		scope = CodexRequestStrategyScopeDedicated
	}
	if policy.Scope != CodexRequestStrategyScopeAll && policy.Scope != scope {
		return policy, false
	}
	return policy, true
}

// applyCodexRouteManagementPolicy copies the request strategy's route controls
// into a WebSocket acquire request. Keeping this at the service boundary lets
// the connection pool remain independent from SettingService while still
// allowing the admin policy to change without a process restart.
func (s *OpenAIGatewayService) applyCodexRouteManagementPolicy(ctx context.Context, scope string, req *openAIWSAcquireRequest) {
	if s == nil || req == nil || req.Account == nil || !req.Account.IsOpenAIOAuthLike() {
		return
	}
	req.CookiePolicyScope = scope
	account := req.Account
	req.CookiePolicyCurrent = func(checkCtx context.Context) string {
		return s.codexCookieModeForRequest(withCodexRequestStrategyConnectionScope(checkCtx, scope), account)
	}
	req.CookieMode = CodexCookiePreserve
	policy, enabled := s.codexRequestStrategyPolicyForScope(ctx, scope)
	if enabled {
		req.CookieMode = normalizeCodexCookieMode(policy.CookieMode)
	}
	if !enabled || policy.RouteAffinityMode == CodexRouteAffinityOff {
		return
	}
	req.RouteAffinityMode = policy.RouteAffinityMode
	req.RoutePrewarmConnections = policy.RoutePrewarmConnections
	req.RouteFailureCooldownSeconds = policy.RouteFailureCooldownSeconds
	if policy.RouteAffinityMode == CodexRouteAffinityStrict && req.CodexTicketReceipt != nil {
		req.RouteQualityBlocked = s.codexModelQualityPaused(ctx, req.Account, req.CodexTicketReceipt.ticket.Model)
	}
}

func (s *OpenAIGatewayService) applyCodexRequestBodyPolicyForScope(ctx context.Context, body []byte, account *Account, scope string) ([]byte, bool, error) {
	if account == nil || !account.IsOpenAI() {
		return body, false, nil
	}
	policy, enabled := s.codexRequestStrategyPolicyForScope(ctx, scope)
	if !enabled {
		return body, false, nil
	}
	return ApplyCodexRequestBodyPolicy(body, policy)
}

const (
	codexAccountRoutingHeader = "x-openai-account-routing-override"
	codexResidencyHeader      = "x-openai-internal-codex-residency"
	codexRegionHeader         = "x-region"
	codexOpenAIRegionHeader   = "x-openai-region"
	codexFedRAMPHeader        = "x-openai-fedramp"
)

var (
	codexRequestEnvironmentContextPattern = regexp.MustCompile(`(?si)<environment_context\b[^>]*>.*?</environment_context>`)
	codexRequestTimeContextElementPattern = regexp.MustCompile(`(?si)<(?:timezone|current_date|current_time_reminder)\b[^>]*>.*?</(?:timezone|current_date|current_time_reminder)>`)
)

// ApplyCodexRequestHeaderPolicy applies region and compliance policy to an
// already constructed upstream header set. Call it after account-derived
// headers have been installed and after untrusted client headers have been
// copied. In preserve mode it leaves existing values untouched.
//
// Region override values are restricted to the values currently accepted by
// Codex account routing. FedRAMP can only be derived from the account; the
// policy deliberately has no force-true mode.
func ApplyCodexRequestHeaderPolicy(headers http.Header, policy CodexRequestStrategyPolicy, account *Account) error {
	policy = normalizeCodexRequestStrategyPolicy(policy)
	if err := ValidateCodexRequestStrategyPolicy(policy); err != nil {
		return err
	}
	if headers == nil {
		return nil
	}

	switch policy.RegionMode {
	case CodexRequestRegionStrip:
		delCodexRegionHeaders(headers)
	case CodexRequestRegionOverride:
		delCodexRegionHeaders(headers)
		if policy.AccountRoutingOverride != "" {
			headers.Set(codexAccountRoutingHeader, policy.AccountRoutingOverride)
		}
		if policy.Residency != "" {
			headers.Set(codexResidencyHeader, policy.Residency)
		}
	}

	switch policy.ComplianceMode {
	case CodexRequestComplianceStrip:
		headers.Del(codexFedRAMPHeader)
	case CodexRequestComplianceAccount:
		// resolveAndSetOpenAIChatGPTAccountHeaders may already have resolved a
		// shadow account to its parent credential. Preserve an existing true
		// value so this helper cannot erase that parent compliance signal.
		if strings.EqualFold(strings.TrimSpace(headers.Get(codexFedRAMPHeader)), "true") {
			break
		}
		if account != nil && account.IsChatGPTAccountFedRAMP() {
			headers.Set(codexFedRAMPHeader, "true")
		} else {
			headers.Del(codexFedRAMPHeader)
		}
	}
	// Apply the cookie policy after account and client headers have been merged.
	// Authentication/session cookies are intentionally preserved by the filter.
	if account != nil && account.IsOpenAIOAuthLike() {
		filterCodexCookieHeader(headers, policy.CookieMode)
	}
	return nil
}

func delCodexRegionHeaders(headers http.Header) {
	headers.Del(codexAccountRoutingHeader)
	headers.Del(codexResidencyHeader)
	headers.Del(codexRegionHeader)
	headers.Del(codexOpenAIRegionHeader)
}

// ApplyCodexRequestBodyPolicy handles known region and time context fields
// already present in a request body. It never derives a country from the
// gateway host and never synthesizes replacement date/time prompts.
func ApplyCodexRequestBodyPolicy(body []byte, policy CodexRequestStrategyPolicy) ([]byte, bool, error) {
	policy = normalizeCodexRequestStrategyPolicy(policy)
	if err := ValidateCodexRequestStrategyPolicy(policy); err != nil {
		return nil, false, err
	}
	changed := false
	if policy.RegionMode == CodexRequestRegionStrip {
		stripped, regionChanged, err := stripCodexRequestRegionContext(body)
		if err != nil {
			return nil, false, err
		}
		body = stripped
		changed = regionChanged
	}
	switch policy.TimeContextMode {
	case CodexRequestTimeContextPreserve:
		return body, changed, nil
	case CodexRequestTimeContextOverride:
		rewritten, timeChanged, err := RewriteOpenAIRequestTimezone(body, policy.Timezone)
		return rewritten, changed || timeChanged, err
	case CodexRequestTimeContextStrip:
		stripped, timeChanged, err := stripCodexRequestTimezone(body)
		return stripped, changed || timeChanged, err
	default:
		return body, changed, nil
	}
}

func stripCodexRequestRegionContext(body []byte) ([]byte, bool, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return body, false, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return body, false, nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return body, false, nil
	}
	if !stripCodexRequestRegionContextValue(&value, nil) {
		return body, false, nil
	}
	rewritten, err := json.Marshal(value)
	if err != nil {
		return nil, false, err
	}
	return rewritten, true, nil
}

func stripCodexRequestRegionContextValue(value *any, path []string) bool {
	switch typed := (*value).(type) {
	case []any:
		changed := false
		for i := range typed {
			item := any(typed[i])
			if stripCodexRequestRegionContextValue(&item, path) {
				typed[i] = item
				changed = true
			}
		}
		return changed
	case map[string]any:
		changed := false
		for key, item := range typed {
			if isCodexRequestRegionContextKey(key, path) {
				delete(typed, key)
				changed = true
				continue
			}
			nextPath := append(append([]string(nil), path...), key)
			if stripCodexRequestRegionContextValue(&item, nextPath) {
				typed[key] = item
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func isCodexRequestRegionContextKey(key string, path []string) bool {
	if len(path) == 0 {
		return key == "market" || key == "locale"
	}
	parent := path[len(path)-1]
	if parent != "user_location" {
		return false
	}
	switch key {
	case "country", "region", "city", "timezone", "utc_offset":
		return len(path) == 1 || (len(path) == 2 && path[0] == "tools")
	default:
		return false
	}
}

func stripCodexRequestTimezone(body []byte) ([]byte, bool, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return body, false, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		// Non-JSON frames are left unchanged; websocket callers may carry
		// protocol control frames alongside JSON response.create payloads.
		return body, false, nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return body, false, nil
	}
	if changed := stripCodexRequestTimezoneValue(&value, nil); !changed {
		return body, false, nil
	} else {
		rewritten, err := json.Marshal(value)
		if err != nil {
			return nil, false, err
		}
		return rewritten, true, nil
	}
}

func stripCodexRequestTimezoneValue(value *any, path []string) bool {
	switch typed := (*value).(type) {
	case string:
		if !isOpenAIRequestTimezoneTextPath(path) {
			return false
		}
		rewritten := codexRequestEnvironmentContextPattern.ReplaceAllStringFunc(typed, func(block string) string {
			return codexRequestTimeContextElementPattern.ReplaceAllString(block, "")
		})
		// Current-time reminders are developer context in Codex. Restrict
		// direct tag removal to the instructions channel so ordinary user text
		// containing an example tag remains byte-for-byte unchanged.
		if len(path) > 0 && path[0] == "instructions" {
			rewritten = codexRequestTimeContextElementPattern.ReplaceAllString(rewritten, "")
		}
		if rewritten == typed {
			return false
		}
		*value = rewritten
		return true
	case []any:
		changed := false
		for i := range typed {
			item := any(typed[i])
			if stripCodexRequestTimezoneValue(&item, path) {
				typed[i] = item
				changed = true
			}
		}
		return changed
	case map[string]any:
		changed := false
		for key, item := range typed {
			if isCodexRequestTimeContextMetadataKey(key, path) {
				delete(typed, key)
				changed = true
				continue
			}
			nextPath := append(append([]string(nil), path...), key)
			if stripCodexRequestTimezoneValue(&item, nextPath) {
				typed[key] = item
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func isCodexRequestTimeContextMetadataKey(key string, path []string) bool {
	switch key {
	case "timezone", "utc_offset", "current_date":
		// These keys are metadata, not arbitrary user content, only under the
		// known tool/location/session containers used by Codex and web search.
		if len(path) == 0 {
			return true
		}
		parent := path[len(path)-1]
		if parent == "user_location" {
			return len(path) == 1 || (len(path) == 2 && path[0] == "tools")
		}
		return (parent == "metadata" || parent == "session") && len(path) == 1
	default:
		return false
	}
}
