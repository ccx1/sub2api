package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

const codexModelQualityResponseLimit = 256 << 10

type codexQualityOutput struct{ text, identity string }

func (s *OpenAIGatewayService) requestCodexModelQuality(ctx context.Context, job *codexModelQualityJob, prompt string) (codexQualityOutput, error) {
	if !s.codexModelQualityCurrent(ctx, job) {
		return codexQualityOutput{}, errors.New("stale")
	}
	session := job.ticket.SessionID
	input := openAICodexTicketProbeInput{Account: job.account, Token: job.account.GetCredential("access_token"),
		Model: job.ticket.Model, ProxyURL: job.proxyURL, State: job.ticket.State, Config: &job.config,
		SubscriptionTier: openAICodexTicketSubscriptionTier(job.account), SessionID: &session,
		BusinessVerification: true, BusinessCredentialSnapshot: job.ticket, FreezeCredentials: true, SkipSchedulerAdmission: true}
	input.BackgroundQuality = true
	req, err := s.buildOpenAICodexTicketProbeRequest(ctx, input)
	if err != nil {
		return codexQualityOutput{}, errors.New("request_unavailable")
	}
	body, _ := json.Marshal(map[string]any{"model": job.ticket.Model, "store": false, "stream": true,
		"instructions": "Complete the task directly. Do not use tools. Return only the requested JSON.",
		"reasoning":    map[string]string{"effort": job.policy.ReasoningEffort},
		"input":        []any{map[string]any{"role": "user", "content": []any{map[string]string{"type": "input_text", "text": prompt}}}}})
	req.Body, req.ContentLength = io.NopCloser(bytes.NewReader(body)), int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	job.ticket.applyHeaders(req.Header)
	setOpenAICodexRoutingHintFromBody(req.Header, job.account, body)
	// 专用传输保留原有 TLS/代理设置；不采集响应 Cookie、不续票、不写采票调度状态。
	resp, err := s.doOpenAICodexTicketProbe(req, input)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return codexQualityOutput{}, errors.New("network_error")
	}
	if resp == nil || resp.Body == nil {
		return codexQualityOutput{}, errors.New("response_incomplete")
	}
	if resp.StatusCode != http.StatusOK {
		return codexQualityOutput{}, codexQualityHTTPError(resp.StatusCode)
	}
	return readCodexQualityOutput(resp.Body, job.ticket.Model)
}

func codexQualityHTTPError(status int) error {
	switch status {
	case 401, 403:
		return errors.New("upstream_auth_error")
	case 429:
		return errors.New("upstream_rate_limited")
	default:
		return errors.New("upstream_error")
	}
}

// 协议完成与模型声明分开处理：缺 model 只意味着身份未知。
func readCodexQualityOutput(reader io.Reader, expected string) (codexQualityOutput, error) {
	data, err := io.ReadAll(io.LimitReader(reader, codexModelQualityResponseLimit+1))
	if err != nil {
		return codexQualityOutput{}, errors.New("response_incomplete")
	}
	if len(data) > codexModelQualityResponseLimit {
		return codexQualityOutput{}, errors.New("response_too_large")
	}
	text := strings.TrimSpace(string(data))
	parser := codexQualityOutputParser{expected: expected, identity: "unknown"}
	if strings.HasPrefix(text, "{") {
		if !gjson.Valid(text) {
			return codexQualityOutput{}, errors.New("response_incomplete")
		}
		parser.inspect(text, "", true)
	} else {
		frames := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n\n")
		for _, frame := range frames[:max(0, len(frames)-1)] {
			var parts []string
			var event string
			for _, line := range strings.Split(frame, "\n") {
				if strings.HasPrefix(line, "data:") {
					parts = append(parts, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
				}
				if strings.HasPrefix(line, "event:") {
					event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				}
			}
			payload := strings.Join(parts, "\n")
			if payload == "" || payload == "[DONE]" {
				continue
			}
			if !gjson.Valid(payload) {
				return codexQualityOutput{}, errors.New("response_incomplete")
			}
			parser.inspect(payload, event, false)
		}
	}
	if parser.failed || !parser.completed {
		return codexQualityOutput{}, errors.New("response_incomplete")
	}
	answer := parser.finalText
	if answer == "" {
		answer = parser.delta.String()
	}
	if strings.TrimSpace(answer) == "" && parser.identity != "mismatch" {
		return codexQualityOutput{}, errors.New("empty_output")
	}
	return codexQualityOutput{text: answer, identity: parser.identity}, nil
}

type codexQualityOutputParser struct {
	expected, identity, finalText string
	completed, failed             bool
	delta                         strings.Builder
}

func (p *codexQualityOutputParser) inspect(payload, event string, plain bool) {
	root := gjson.Parse(payload)
	kind := root.Get("type").String()
	response := root.Get("response")
	if !root.IsObject() || codexQualityFrameFailed(root, kind) || codexQualityFrameFailed(response, event) ||
		event != "" && kind != "" && event != kind {
		p.failed = true
		return
	}
	if kind == "" {
		kind = event
	}
	if kind == "response.output_text.delta" {
		p.delta.WriteString(root.Get("delta").String())
	}
	if plain {
		response = root
	}
	if !plain && kind != "response.completed" {
		return
	}
	if plain && (root.Get("object").String() != "response" || kind != "" && kind != "response.completed") {
		return
	}
	status := response.Get("status").String()
	topStatus := root.Get("status").String()
	modelValue := response.Get("model")
	if !response.IsObject() || status != "completed" && status != "" || topStatus != "" && topStatus != "completed" ||
		modelValue.Exists() && modelValue.Type != gjson.String && modelValue.Type != gjson.Null {
		p.failed = true
		return
	}
	if plain && status != "completed" {
		p.failed = true
		return
	}
	p.completed = true
	model := strings.TrimSpace(response.Get("model").String())
	if model != "" {
		if model != p.expected {
			p.identity = "mismatch"
		} else if p.identity != "mismatch" {
			p.identity = "match"
		}
	}
	var text strings.Builder
	for _, item := range response.Get("output").Array() {
		for _, content := range item.Get("content").Array() {
			if content.Get("type").String() == "output_text" {
				text.WriteString(content.Get("text").String())
			}
		}
	}
	if text.Len() > 0 {
		p.finalText = text.String()
	}
}

func codexQualityFrameFailed(frame gjson.Result, kind string) bool {
	status := frame.Get("status").String()
	return openAICodexTicketResponseFailed(kind, status, json.RawMessage(frame.Get("error").Raw)) ||
		kind == "response.cancelled" || kind == "response.canceled" || status == "cancelled" || status == "canceled"
}
