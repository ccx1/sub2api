package service

import (
	"net/http"
	"strings"
	"testing"
)

func TestApplyCodexRequestHeaderPolicyPreservesAccountCompliance(t *testing.T) {
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_is_fedramp": true},
	}
	headers := make(http.Header)
	headers.Set(codexAccountRoutingHeader, "us_cr")
	headers.Set(codexFedRAMPHeader, "false")
	policy := DefaultCodexRequestStrategyPolicy()
	policy.RegionMode = CodexRequestRegionPreserve
	policy.TimeContextMode = CodexRequestTimeContextPreserve
	if err := ApplyCodexRequestHeaderPolicy(headers, policy, account); err != nil {
		t.Fatalf("apply policy: %v", err)
	}
	if got := headers.Get(codexAccountRoutingHeader); got != "us_cr" {
		t.Fatalf("preserve mode changed routing header: %q", got)
	}
	if got := headers.Get(codexFedRAMPHeader); got != "true" {
		t.Fatalf("account mode did not derive FedRAMP header: %q", got)
	}
}

func TestApplyCodexRequestHeaderPolicyOverrideAndStrip(t *testing.T) {
	headers := make(http.Header)
	headers.Set(codexRegionHeader, "CN")
	headers.Set(codexOpenAIRegionHeader, "cn")
	policy := DefaultCodexRequestStrategyPolicy()
	policy.RegionMode = CodexRequestRegionOverride
	policy.AccountRoutingOverride = "us"
	policy.Residency = "us"
	policy.ComplianceMode = CodexRequestComplianceStrip
	if err := ApplyCodexRequestHeaderPolicy(headers, policy, nil); err != nil {
		t.Fatalf("apply override policy: %v", err)
	}
	if got := headers.Get(codexAccountRoutingHeader); got != "us" {
		t.Fatalf("unexpected routing override: %q", got)
	}
	if got := headers.Get(codexResidencyHeader); got != "us" {
		t.Fatalf("unexpected residency override: %q", got)
	}
	if got := headers.Get(codexFedRAMPHeader); got != "" {
		t.Fatalf("compliance strip retained header: %q", got)
	}

	policy.RegionMode = CodexRequestRegionStrip
	headers.Set(codexAccountRoutingHeader, "us")
	headers.Set(codexResidencyHeader, "us")
	if err := ApplyCodexRequestHeaderPolicy(headers, policy, nil); err != nil {
		t.Fatalf("apply strip policy: %v", err)
	}
	if headers.Get(codexAccountRoutingHeader) != "" || headers.Get(codexResidencyHeader) != "" || headers.Get(codexRegionHeader) != "" || headers.Get(codexOpenAIRegionHeader) != "" {
		t.Fatal("region strip retained protected headers")
	}
}

func TestApplyCodexRequestHeaderPolicyFiltersInfrastructureCookies(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	headers := http.Header{"Cookie": {
		"__oailb=route; __cflb=lb; cf_clearance=passed; cf_chl_rc_i=challenge; chatgpt_session=auth==; unknown=value",
	}}
	policy := DefaultCodexRequestStrategyPolicy()
	policy.CookieMode = CodexCookieStripInfrastructure
	if err := ApplyCodexRequestHeaderPolicy(headers, policy, account); err != nil {
		t.Fatalf("apply cookie policy: %v", err)
	}
	if got := headers.Get("Cookie"); got != "chatgpt_session=auth==; unknown=value" {
		t.Fatalf("unexpected filtered cookies: %q", got)
	}
}

func TestApplyCodexRequestBodyPolicyTimezoneModes(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","content":"<environment_context><timezone>Asia/Shanghai</timezone><current_date>2026-09-24</current_date><current_time_reminder>It is 08:00 UTC.</current_time_reminder></environment_context>"}],"metadata":{"timezone":"Asia/Shanghai","current_date":"2026-09-24","utc_offset":"+08:00"},"tools":[{"user_location":{"country":"CN","timezone":"Asia/Shanghai","utc_offset":"+08:00"}}]}`)

	preserve := DefaultCodexRequestStrategyPolicy()
	preserve.RegionMode = CodexRequestRegionPreserve
	preserve.TimeContextMode = CodexRequestTimeContextPreserve
	unchanged, changed, err := ApplyCodexRequestBodyPolicy(body, preserve)
	if err != nil || changed || string(unchanged) != string(body) {
		t.Fatalf("preserve mode changed body: changed=%v err=%v body=%s", changed, err, unchanged)
	}

	override := preserve
	override.TimeContextMode = CodexRequestTimeContextOverride
	override.Timezone = "America/New_York"
	rewritten, changed, err := ApplyCodexRequestBodyPolicy(body, override)
	if err != nil || !changed {
		t.Fatalf("override mode did not rewrite body: changed=%v err=%v", changed, err)
	}
	if got := string(rewritten); got == string(body) || !containsAll(got, "America/New_York") {
		t.Fatalf("override timezone missing: %s", got)
	}

	strip := preserve
	strip.RegionMode = CodexRequestRegionStrip
	strip.TimeContextMode = CodexRequestTimeContextStrip
	stripped, changed, err := ApplyCodexRequestBodyPolicy(body, strip)
	if err != nil || !changed {
		t.Fatalf("strip mode did not change body: changed=%v err=%v", changed, err)
	}
	if got := string(stripped); containsAnyCodex(got, "Asia/Shanghai", "2026-09-24", "current_time_reminder", "+08:00", `"country"`) {
		t.Fatalf("strip mode retained time context: %s", got)
	}
}

func TestStripCodexRequestBodyPolicyDoesNotRewriteOrdinaryUserText(t *testing.T) {
	policy := DefaultCodexRequestStrategyPolicy()
	body := []byte(`{"input":"Please keep <current_date>2026-09-24</current_date> in this quoted example."}`)
	stripped, changed, err := ApplyCodexRequestBodyPolicy(body, policy)
	if err != nil {
		t.Fatal(err)
	}
	if changed || string(stripped) != string(body) {
		t.Fatalf("ordinary user text was changed: changed=%v body=%s", changed, stripped)
	}
}

func TestStripCodexRequestBodyPolicyRemovesInstructionReminder(t *testing.T) {
	policy := DefaultCodexRequestStrategyPolicy()
	body := []byte(`{"instructions":"<current_time_reminder>It is 2026-09-24 08:00 UTC.</current_time_reminder>"}`)
	stripped, changed, err := ApplyCodexRequestBodyPolicy(body, policy)
	if err != nil || !changed || strings.Contains(string(stripped), "current_time_reminder") {
		t.Fatalf("instruction reminder was not removed: changed=%v err=%v body=%s", changed, err, stripped)
	}
}

func TestValidateCodexRequestStrategyPolicyRejectsUnsafeOverrides(t *testing.T) {
	policy := DefaultCodexRequestStrategyPolicy()
	policy.RegionMode = CodexRequestRegionOverride
	if err := ValidateCodexRequestStrategyPolicy(policy); err == nil {
		t.Fatal("region override without a value should fail")
	}
	policy = DefaultCodexRequestStrategyPolicy()
	policy.ComplianceMode = "force"
	if err := ValidateCodexRequestStrategyPolicy(policy); err == nil {
		t.Fatal("unsupported compliance force mode should fail")
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

func containsAnyCodex(value string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(value, part) {
			return true
		}
	}
	return false
}
