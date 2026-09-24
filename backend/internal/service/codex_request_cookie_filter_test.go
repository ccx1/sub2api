package service

import (
	"net/http"
	"reflect"
	"testing"
)

func TestNormalizeCodexCookieModeKeepsInvalidValues(t *testing.T) {
	for input, expected := range map[string]string{
		"": CodexCookiePreserve, CodexCookiePreserve: CodexCookiePreserve,
		CodexCookieStripRouting: CodexCookieStripRouting, "unknown": "unknown",
		" PRESERVE ": " PRESERVE ", " ": " ",
	} {
		if got := normalizeCodexCookieMode(input); got != expected {
			t.Errorf("normalize %q: got %q, want %q", input, got, expected)
		}
	}
}

func TestCodexCookieExcludedExactInfrastructureNames(t *testing.T) {
	groups := []struct {
		names   []string
		routing bool
		cf      bool
	}{
		{[]string{"__oailb", "__cflb"}, true, false},
		{[]string{"__cf_bm", "__cfruid", "__cfseq", "__cfwaitingroom", "_cfuvid",
			"cf_clearance", "cf_ob_info", "cf_use_ob", "cf_chl_rc_i", "cf_chl_"}, false, true},
		{[]string{"chatgpt_session", "oai-auth-token", "__Secure-next-auth.session-token",
			"unknown", "__OAILB", "CF_CHL_rc_i", "CF_CLEARANCE", "x__oailb", "__oailb_extra",
			"cf_clearance_token", "my_cf_chl_rc_i", "cf_chl", ""}, false, false},
	}
	for _, group := range groups {
		for _, name := range group.names {
			for mode, expected := range map[string]bool{
				CodexCookiePreserve: false, "": false, "unknown": false,
				CodexCookieStripRouting: group.routing, CodexCookieStripCloudflare: group.cf,
				CodexCookieStripInfrastructure: group.routing || group.cf,
			} {
				if got := codexCookieExcluded(mode, name); got != expected {
					t.Errorf("mode %q cookie %q: got %v, want %v", mode, name, got, expected)
				}
			}
		}
	}
}

func TestFilterCodexCookieHeaderModes(t *testing.T) {
	const input = "__oailb=route; __cflb=lb; cf_clearance=passed; cf_chl_rc_i=challenge; chatgpt_session=auth=="
	for mode, expected := range map[string]string{
		CodexCookiePreserve:            input,
		CodexCookieStripRouting:        "cf_clearance=passed; cf_chl_rc_i=challenge; chatgpt_session=auth==",
		CodexCookieStripCloudflare:     "__oailb=route; __cflb=lb; chatgpt_session=auth==",
		CodexCookieStripInfrastructure: "chatgpt_session=auth==",
	} {
		t.Run(mode, func(t *testing.T) {
			headers := http.Header{"Cookie": {input}}
			filterCodexCookieHeader(headers, mode)
			if got := headers.Get("Cookie"); got != expected {
				t.Errorf("got %q, want %q", got, expected)
			}
		})
	}
}

func TestFilterCodexCookieHeaderPreservesUnselectedValues(t *testing.T) {
	headers := http.Header{
		"Cookie": {"__oailb=route; chatgpt_session=abc==; oai-auth-token=def==",
			"__Secure-next-auth.session-token=ghi; quoted=\"a=b\"; cf_clearance=passed",
			"__cf_bm=bot; __cflb=lb"},
		"cookie":        {"cf_chl_rc_i=challenge; unknown=__oailb=inside"},
		"COOKIE":        {"__OAILB=case; x__oailb=prefix; cf_clearance_token=suffix"},
		"Authorization": {"Bearer account-token"},
		"Set-Cookie":    {"__oailb=response-route; Secure"},
	}
	want := http.Header{
		"Cookie": {"chatgpt_session=abc==; oai-auth-token=def==",
			"__Secure-next-auth.session-token=ghi; quoted=\"a=b\""},
		"cookie":        {"unknown=__oailb=inside"},
		"COOKIE":        {"__OAILB=case; x__oailb=prefix; cf_clearance_token=suffix"},
		"Authorization": {"Bearer account-token"},
		"Set-Cookie":    {"__oailb=response-route; Secure"},
	}
	originalValues := headers["Cookie"]
	original := headers.Clone()
	filterCodexCookieHeader(headers, CodexCookieStripInfrastructure)
	if !reflect.DeepEqual(headers, want) {
		t.Errorf("filtered headers: got %#v, want %#v", headers, want)
	}
	if !reflect.DeepEqual(originalValues, original["Cookie"]) {
		t.Error("filter mutated the original header value slice")
	}
}

func TestFilterCodexCookieHeaderRemovesEmptyHeaders(t *testing.T) {
	headers := http.Header{"Cookie": {"__oailb=route; __cf_bm=bot", " ; cf_clearance=passed; "}}
	filterCodexCookieHeader(headers, CodexCookieStripInfrastructure)
	if _, exists := headers["Cookie"]; exists {
		t.Errorf("all excluded cookies must remove the Cookie header: %#v", headers)
	}
	filterCodexCookieHeader(nil, CodexCookieStripInfrastructure)
}

func TestFilterCodexCookieHeaderNoOpRetainsBytes(t *testing.T) {
	for _, mode := range []string{"", CodexCookiePreserve, "invalid"} {
		headers := http.Header{"Cookie": {" __oailb=route ; auth =\"a=b\";  ", "", "cf_chl_rc_i=value"}}
		before := headers.Clone()
		filterCodexCookieHeader(headers, mode)
		if !reflect.DeepEqual(headers, before) {
			t.Errorf("mode %q changed original bytes: %#v", mode, headers)
		}
	}
	headers := http.Header{"Cookie": {" auth =\"a=b\"; ; unknown=1  "}}
	before := headers.Clone()
	filterCodexCookieHeader(headers, CodexCookieStripInfrastructure)
	if !reflect.DeepEqual(headers, before) {
		t.Errorf("header without excluded names changed: %#v", headers)
	}
}

func TestFilterCodexCookiesPreservesInputAndScopedDuplicates(t *testing.T) {
	root := &http.Cookie{Name: "chatgpt_session", Value: "root", Path: "/", Secure: true}
	scoped := &http.Cookie{Name: "chatgpt_session", Value: "scoped", Path: "/backend-api", HttpOnly: true}
	routing := &http.Cookie{Name: "__oailb", Value: "route", Path: "/"}
	cloudflare := &http.Cookie{Name: "cf_clearance", Value: "passed", Path: "/"}
	cookies := []*http.Cookie{routing, root, cloudflare, scoped, nil}
	before := append([]*http.Cookie(nil), cookies...)
	for mode, want := range map[string][]*http.Cookie{
		CodexCookiePreserve:            cookies,
		CodexCookieStripRouting:        {root, cloudflare, scoped, nil},
		CodexCookieStripCloudflare:     {routing, root, scoped, nil},
		CodexCookieStripInfrastructure: {root, scoped, nil},
	} {
		got := filterCodexCookies(cookies, mode)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("mode %q: got %#v, want %#v", mode, got, want)
		}
		if mode != CodexCookiePreserve {
			got[0] = nil
		}
		if !reflect.DeepEqual(cookies, before) {
			t.Errorf("mode %q mutated the input slice", mode)
		}
	}
	if got := filterCodexCookies(nil, CodexCookieStripInfrastructure); got != nil {
		t.Errorf("nil input must stay nil: %#v", got)
	}
	if got := filterCodexCookies([]*http.Cookie{routing, cloudflare}, CodexCookieStripInfrastructure); len(got) != 0 {
		t.Errorf("all excluded cookies remain: %#v", got)
	}
}
