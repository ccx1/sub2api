package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// 未实现的方法会 panic；预览只能执行 GetByID 只读查询。
type ticketPreviewReadOnlyRepo struct {
	AccountRepository
	account *Account
	reads   int
}

func (r *ticketPreviewReadOnlyRepo) GetByID(context.Context, int64) (*Account, error) {
	r.reads++
	return r.account, nil
}

func ticketPreviewService(t *testing.T) (*OpenAIGatewayService, *ticketPreviewReadOnlyRepo) {
	t.Helper()
	account := ticketTestAccount(41)
	account.Status = StatusDisabled
	account.Credentials["refresh_token"] = "private-refresh-value"
	account.Credentials["id_token"] = "private-id-value"
	repo := &ticketPreviewReadOnlyRepo{account: account}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"}}, &codexTicketFuncUpstream{
		do: func(*http.Request) (*http.Response, error) { panic("preview must not send a request") },
	})
	svc.accountRepo = repo
	return svc, repo
}

func TestCodexTicketPreviewReadOnlySingleBaselineAndRedaction(t *testing.T) {
	svc, repo := ticketPreviewService(t)
	before, err := json.Marshal(repo.account)
	require.NoError(t, err)
	result, err := svc.PreviewOpenAICodexTicketRequest(context.Background(), 41, CodexTicketRequestPreviewInput{Model: "gpt-6-astra"})
	require.NoError(t, err)
	require.Empty(t, result.ValidationErrors)
	require.False(t, result.Sent)
	require.Empty(t, result.Changes)
	require.Equal(t, result.BeforeHeaders, result.AfterHeaders)
	require.Equal(t, 1, repo.reads)
	require.NotEmpty(t, http.Header(result.BeforeHeaders).Get("session_id"))
	require.Equal(t, "Bearer preview-access-token", http.Header(result.BeforeHeaders).Get("Authorization"))
	require.Equal(t, "preview-account-id", http.Header(result.BeforeHeaders).Get("Chatgpt-Account-Id"))
	after, err := json.Marshal(repo.account)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	for _, secret := range []string{"private-refresh-value", "private-id-value", "acc-1"} {
		require.NotContains(t, string(encoded), secret)
	}
	sources := map[string]bool{}
	for _, source := range result.HeaderSources {
		sources[source.Source] = true
	}
	require.True(t, sources["probe_defaults"])
	require.True(t, sources["account_identity"])
	require.True(t, sources["codex_identity"])
}

func TestCodexTicketPreviewChangesAndRejectedRulesAreAtomic(t *testing.T) {
	svc, _ := ticketPreviewService(t)
	input := CodexTicketRequestPreviewInput{Model: "gpt-6-astra", Set: []CodexTicketHeaderSet{{Name: "aCcEpT-EnCoDiNg", Value: "gzip"}}}
	result, err := svc.PreviewOpenAICodexTicketRequest(context.Background(), 41, input)
	require.NoError(t, err)
	require.Empty(t, result.ValidationErrors)
	require.Len(t, result.Changes, 1)
	require.Equal(t, "changed", result.Changes[0].Kind)
	require.Equal(t, "gzip", http.Header(result.AfterHeaders).Get("Accept-Encoding"))
	require.Equal(t, "experiment", result.HeaderSources[len(result.HeaderSources)-1].Source)
	input.Remove = []string{"Authorization"}
	result, err = svc.PreviewOpenAICodexTicketRequest(context.Background(), 41, input)
	require.NoError(t, err)
	require.NotEmpty(t, result.ValidationErrors)
	require.Equal(t, result.BeforeHeaders, result.AfterHeaders)
	require.Empty(t, result.Changes)
	input.Set = []CodexTicketHeaderSet{{Name: "User-Agent", Value: "codex_cli_rs/0.1.0"}, {Name: "Version", Value: "0.2.0"}}
	input.Remove = nil
	result, err = svc.PreviewOpenAICodexTicketRequest(context.Background(), 41, input)
	require.NoError(t, err)
	require.NotEmpty(t, result.ValidationErrors)
	require.Equal(t, result.BeforeHeaders, result.AfterHeaders)
}

func TestCodexTicketPreviewValidationAndEligibility(t *testing.T) {
	for _, tc := range []CodexTicketRequestPreviewInput{
		{Set: []CodexTicketHeaderSet{{Name: "User-Agent", Value: "ok\r\nbad"}}},
		{Set: []CodexTicketHeaderSet{{Name: "Originator", Value: "x\x00"}}},
		{Set: []CodexTicketHeaderSet{{Name: "Originator", Value: "x\t"}}},
		{Set: []CodexTicketHeaderSet{{Name: "Originator", Value: strings.Repeat("a", 2049)}}},
		{Set: []CodexTicketHeaderSet{{Name: "Originator", Value: "x"}, {Name: "originator", Value: "y"}}},
		{Set: []CodexTicketHeaderSet{{Name: "Originator", Value: "x"}}, Remove: []string{"ORIGINATOR"}},
		{Set: []CodexTicketHeaderSet{{Name: " Originator", Value: "x"}}},
		{Remove: []string{"Cookie"}},
		{Remove: make([]string, 33)},
	} {
		require.NotEmpty(t, validateCodexTicketPreviewRules(tc))
	}
	require.Empty(t, validateCodexTicketPreviewRules(CodexTicketRequestPreviewInput{Set: []CodexTicketHeaderSet{{Name: "Originator", Value: strings.Repeat("a", 2048)}}}))
	svc, repo := ticketPreviewService(t)
	result, err := svc.PreviewOpenAICodexTicketRequest(context.Background(), 41, CodexTicketRequestPreviewInput{Model: "other"})
	require.NoError(t, err)
	require.NotEmpty(t, result.ValidationErrors)
	require.Empty(t, result.BeforeHeaders)
	repo.account.Type = AccountTypeAPIKey
	_, err = svc.PreviewOpenAICodexTicketRequest(context.Background(), 41, CodexTicketRequestPreviewInput{Model: "gpt-6-astra"})
	require.ErrorIs(t, err, ErrCodexTicketPreviewUnavailable)
}
