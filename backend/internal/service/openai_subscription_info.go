package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
)

type chatGPTSubscriptionInfo struct {
	PlanType               string `json:"plan_type"`
	ActiveUntil            string `json:"active_until"`
	WillRenew              bool   `json:"will_renew"`
	ID                     string `json:"id"`
	BillingCurrency        string `json:"billing_currency"`
	PriceCountry           string `json:"price_country"`
	BillingMetadataChecked bool   `json:"-"`
}

func (s *chatGPTSubscriptionInfo) UnmarshalJSON(data []byte) error {
	type plain chatGPTSubscriptionInfo
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	_, currencyPresent := fields["billing_currency"]
	_, countryPresent := fields["price_country"]
	*s = chatGPTSubscriptionInfo(decoded)
	// 两项证据成对替换；缺字段不等于上游明确清空，保留已有资料。
	s.BillingMetadataChecked = currencyPresent && countryPresent
	s.BillingCurrency = strings.ToUpper(strings.TrimSpace(s.BillingCurrency))
	s.PriceCountry = normalizeProxyRegionCountry(s.PriceCountry)
	return nil
}

func fetchChatGPTSubscriptionInfo(ctx context.Context, factory PrivacyClientFactory, token, proxyURL, accountID string) *chatGPTSubscriptionInfo {
	accountID = strings.TrimSpace(accountID)
	if token == "" || accountID == "" || factory == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client, err := factory(proxyURL)
	if err != nil {
		return nil
	}
	var result chatGPTSubscriptionInfo
	response, err := client.R().SetContext(ctx).
		SetHeader("Authorization", "Bearer "+token).
		SetHeader("Origin", "https://chatgpt.com").SetHeader("Referer", "https://chatgpt.com/").
		SetHeader("Accept", "application/json").SetSuccessResult(&result).
		SetQueryParam("account_id", accountID).Get(chatGPTSubscriptionsURL)
	if err != nil {
		slog.Debug("chatgpt_subscription_request_failed", "error", err.Error())
		return nil
	}
	if !response.IsSuccessState() {
		slog.Debug("chatgpt_subscription_failed", "status", response.StatusCode)
		return nil
	}
	result.ActiveUntil = strings.TrimSpace(result.ActiveUntil)
	if _, err := time.Parse(time.RFC3339, result.ActiveUntil); err != nil {
		result.ActiveUntil = ""
	}
	return &result
}

func applyChatGPTBillingMetadata(tokenInfo *OpenAITokenInfo, subscription *chatGPTSubscriptionInfo) {
	if subscription == nil || !subscription.BillingMetadataChecked || strings.TrimSpace(tokenInfo.ChatGPTAccountID) == "" {
		return
	}
	tokenInfo.BillingCurrency = subscription.BillingCurrency
	tokenInfo.PriceCountry = subscription.PriceCountry
	tokenInfo.BillingMetadataChecked = true
}
