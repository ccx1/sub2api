package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestFetchChatGPTSubscriptionInfo_BillingRegion(t *testing.T) {
	for _, tc := range []struct {
		name, body, currency, country string
		checked                       bool
	}{
		{"yen with null country", `{"plan_type":"pro","billing_currency":"JPY","price_country":null}`, "JPY", "", true},
		{"explicit country", `{"billing_currency":"usd","price_country":"ph"}`, "USD", "PH", true},
		{"old response without billing", `{"plan_type":"pro"}`, "", "", false},
		{"partial currency does not erase known country", `{"billing_currency":"JPY"}`, "JPY", "", false},
		{"partial country does not erase known currency", `{"price_country":"PH"}`, "", "PH", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "personal-account", r.URL.Query().Get("account_id"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			old := chatGPTSubscriptionsURL
			chatGPTSubscriptionsURL = server.URL
			t.Cleanup(func() { chatGPTSubscriptionsURL = old })
			info := fetchChatGPTSubscriptionInfo(context.Background(), func(string) (*req.Client, error) { return req.C().SetTimeout(time.Second), nil }, "token", "", "personal-account")
			require.NotNil(t, info)
			require.Equal(t, tc.currency, info.BillingCurrency)
			require.Equal(t, tc.country, info.PriceCountry)
			require.Equal(t, tc.checked, info.BillingMetadataChecked)
		})
	}
}

func TestBuildAccountCredentials_BillingMetadataPreservesFailureAndClearsAbsentCountry(t *testing.T) {
	svc := &OpenAIOAuthService{}
	current := map[string]any{"billing_currency": "USD", "price_country": "US"}
	failed := svc.BuildAccountCredentials(&OpenAITokenInfo{AccessToken: "new"})
	require.NotContains(t, failed, "billing_currency")
	require.Equal(t, "US", MergeCredentials(current, failed)["price_country"])
	verified := svc.BuildAccountCredentials(&OpenAITokenInfo{AccessToken: "new", BillingMetadataChecked: true, BillingCurrency: "JPY"})
	merged := MergeCredentials(current, verified)
	require.Equal(t, "JPY", merged["billing_currency"])
	require.Equal(t, "", merged["price_country"])
}

func TestEnrichTokenInfo_BillingBelongsToPersonalSubscription(t *testing.T) {
	server := newChatGPTBackendTestServer(t, chatGPTBackendTestServerConfig{
		accountsCheck: map[string]any{"accounts": map[string]any{"workspace": map[string]any{
			"account":     map[string]any{"account_id": "workspace", "plan_type": "pro", "is_default": true},
			"entitlement": map[string]any{"billing_currency": "PHP", "expires_at": "2027-01-01T00:00:00Z"},
		}}},
		onSubscription: func(id string) map[string]any {
			require.Equal(t, "personal", id)
			return map[string]any{"billing_currency": "JPY", "price_country": nil, "active_until": "2027-02-01T00:00:00Z"}
		},
	})
	defer server.Close()
	svc := &OpenAIOAuthService{privacyClientFactory: func(string) (*req.Client, error) {
		client := req.C().SetTimeout(time.Second)
		client.OnBeforeRequest(func(_ *req.Client, r *req.Request) error {
			if r.Method == http.MethodPatch {
				r.RawURL = server.URL + "/ignored-setting"
			}
			return nil
		})
		return client, nil
	}}
	info := &OpenAITokenInfo{AccessToken: "token", ChatGPTAccountID: "personal", OrganizationID: "workspace", PlanType: "pro"}
	svc.enrichTokenInfo(context.Background(), info, "")
	require.True(t, info.BillingMetadataChecked)
	require.Equal(t, "JPY", info.BillingCurrency)
	require.Empty(t, info.PriceCountry)
	encoded, err := json.Marshal(info)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"billing_metadata_checked":true`)
}
