package service

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strings"
)

type CodexTicketHeaderSet struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type CodexTicketRequestPreviewInput struct {
	Model  string                 `json:"model"`
	Set    []CodexTicketHeaderSet `json:"set"`
	Remove []string               `json:"remove"`
}
type CodexTicketHeaderChange struct {
	Name   string   `json:"name"`
	Before []string `json:"before"`
	After  []string `json:"after"`
	Kind   string   `json:"kind"`
}
type CodexTicketPreviewValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}
type CodexTicketHeaderSource struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Reason string `json:"reason"`
}
type CodexTicketRequestPreview struct {
	BeforeHeaders    map[string][]string                 `json:"before_headers"`
	AfterHeaders     map[string][]string                 `json:"after_headers"`
	Changes          []CodexTicketHeaderChange           `json:"changes"`
	ValidationErrors []CodexTicketPreviewValidationError `json:"validation_errors"`
	HeaderSources    []CodexTicketHeaderSource           `json:"header_sources"`
	Sent             bool                                `json:"sent"`
}

var ErrCodexTicketPreviewUnavailable = errors.New("codex ticket request preview unavailable for this account")

func (s *OpenAIGatewayService) PreviewOpenAICodexTicketRequest(ctx context.Context, accountID int64, input CodexTicketRequestPreviewInput) (CodexTicketRequestPreview, error) {
	result := CodexTicketRequestPreview{BeforeHeaders: map[string][]string{}, AfterHeaders: map[string][]string{}, Changes: []CodexTicketHeaderChange{}, ValidationErrors: []CodexTicketPreviewValidationError{}, HeaderSources: []CodexTicketHeaderSource{}}
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return result, ErrCodexTicketPreviewUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return result, err
	}
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsShadow() {
		return result, ErrCodexTicketPreviewUnavailable
	}
	model := normalizeOpenAICodexTicketModel(input.Model)
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	if model == "" || !codexTicketConfigGatesModel(cfg, model) {
		result.ValidationErrors = append(result.ValidationErrors, CodexTicketPreviewValidationError{Field: "model", Message: "Model is not configured for Codex ticket harvesting"})
		return result, nil
	}
	previewAccount := cloneOpenAICodexTicketAccount(account)
	previewAccount.Credentials = cloneAnyMap(account.Credentials)
	if previewAccount.Credentials == nil {
		previewAccount.Credentials = map[string]any{}
	}
	if account.GetChatGPTAccountID() != "" {
		previewAccount.Credentials["chatgpt_account_id"] = "preview-account-id"
	}
	for _, key := range []string{"access_token", "refresh_token", "id_token", "api_key"} {
		delete(previewAccount.Credentials, key)
	}
	req, err := s.buildOpenAICodexTicketProbeRequest(ctx, openAICodexTicketProbeInput{Account: previewAccount, Token: "preview-access-token", Model: model, Config: &cfg, HeaderSources: &result.HeaderSources})
	if err != nil {
		return result, err
	}
	defer req.Body.Close()
	result.BeforeHeaders = map[string][]string(req.Header.Clone())
	result.AfterHeaders = map[string][]string(req.Header.Clone())
	result.ValidationErrors = validateCodexTicketPreviewRules(input)
	if len(result.ValidationErrors) > 0 {
		return result, nil
	}
	after := http.Header(result.AfterHeaders)
	for _, rule := range input.Set {
		after.Set(rule.Name, rule.Value)
	}
	for _, name := range input.Remove {
		after.Del(name)
	}
	result.ValidationErrors = validateCodexTicketPreviewIdentity(after, model)
	if len(result.ValidationErrors) > 0 {
		result.AfterHeaders = map[string][]string(req.Header.Clone())
		return result, nil
	}
	result.Changes = codexTicketPreviewChanges(req.Header, after)
	recordCodexTicketHeaderSources(&result.HeaderSources, req.Header, after, "experiment", "Temporary preview rule; not saved or sent")
	return result, nil
}

func recordCodexTicketHeaderSources(target *[]CodexTicketHeaderSource, before, after http.Header, source, reason string) {
	if target == nil {
		return
	}
	for _, change := range codexTicketPreviewChanges(before, after) {
		*target = append(*target, CodexTicketHeaderSource{Name: change.Name, Source: source, Reason: reason})
	}
}

func codexTicketPreviewChanges(before, after http.Header) []CodexTicketHeaderChange {
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	names := make([]string, 0, len(keys))
	for key := range keys {
		names = append(names, key)
	}
	sort.Strings(names)
	changes := make([]CodexTicketHeaderChange, 0)
	for _, name := range names {
		if reflect.DeepEqual(before[name], after[name]) {
			continue
		}
		kind := "changed"
		if len(before[name]) == 0 {
			kind = "added"
		} else if len(after[name]) == 0 {
			kind = "removed"
		}
		changes = append(changes, CodexTicketHeaderChange{Name: name, Before: append([]string{}, before[name]...), After: append([]string{}, after[name]...), Kind: kind})
	}
	return changes
}

func codexTicketPreviewHeaderSnapshot(in openAICodexTicketProbeInput, headers http.Header) http.Header {
	if in.HeaderSources == nil {
		return nil
	}
	return headers.Clone()
}

func validateCodexTicketPreviewIdentity(headers http.Header, model string) []CodexTicketPreviewValidationError {
	result := []CodexTicketPreviewValidationError{}
	if hint := headers.Get(openAICodexRoutingHintHeader); hint != "" && strings.Split(hint, ";")[0] != "model="+model {
		result = append(result, CodexTicketPreviewValidationError{Field: "x-codex-routing-hint", Message: "Routing hint must match the selected model"})
	}
	ua, version := headers.Get("User-Agent"), headers.Get("Version")
	if strings.HasPrefix(ua, "codex_") && strings.Contains(ua, "/") && version != "" {
		parts := strings.SplitN(ua, "/", 2)
		if fields := strings.Fields(parts[1]); len(fields) > 0 && fields[0] != version {
			result = append(result, CodexTicketPreviewValidationError{Field: "version", Message: "Version does not match the Codex User-Agent"})
		}
	}
	return result
}
