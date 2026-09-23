package service

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/codextickettrace"
)

const (
	codexTicketCaptureLimit = 64 << 10
	codexTicketBodyLimit    = codexTicketCaptureLimit
	codexTicketHeaderLimit  = 16 << 10
)

type CodexTicketHTTPMessage struct {
	Method           string              `json:"method,omitempty"`
	URL              string              `json:"url,omitempty"`
	StatusCode       int                 `json:"status_code,omitempty"`
	Headers          map[string][]string `json:"headers,omitempty"`
	Body             string              `json:"body,omitempty"`
	BodyBytes        int64               `json:"body_bytes"`
	BodyTruncated    bool                `json:"body_truncated,omitempty"`
	HeadersTruncated bool                `json:"headers_truncated,omitempty"`
}

type CodexTicketExchange struct {
	CaptureMode      string                       `json:"capture_mode,omitempty"`
	RequestedModel   string                       `json:"requested_model"`
	ReportedModels   []string                     `json:"reported_models,omitempty"`
	ModelsTruncated  bool                         `json:"models_truncated,omitempty"`
	Request          *CodexTicketHTTPMessage      `json:"request,omitempty"`
	Response         *CodexTicketHTTPMessage      `json:"response,omitempty"`
	Network          *CodexTicketNetwork          `json:"network,omitempty"`
	ModelDeclaration *CodexTicketModelDeclaration `json:"model_declaration,omitempty"`
	UpstreamError    *CodexTicketUpstreamError    `json:"upstream_error,omitempty"`
	Signals          *CodexTicketSignals          `json:"signals,omitempty"`
}

type codexTicketExchangeCapture struct {
	exchange *CodexTicketExchange
	network  *codextickettrace.Recorder
}

// 仅合成打票探测提供 Attempt；实验报文只写管理员专用历史，不写通用日志。
func startCodexTicketExchange(in openAICodexTicketProbeInput, req *http.Request) *codexTicketExchangeCapture {
	if in.Attempt == nil || req == nil {
		return nil
	}
	c := &codexTicketExchangeCapture{exchange: &CodexTicketExchange{CaptureMode: "raw", RequestedModel: in.Model}}
	ctx, network := codextickettrace.WithRecorder(req.Context())
	*req = *req.WithContext(ctx)
	c.network = network
	if !codexTicketProbeBusiness(in) {
		in.Attempt.HarvestExchange = c.exchange
	} else {
		in.Attempt.BusinessExchange = c.exchange
	}
	if codexTicketQualityEnabled(in) {
		// 质量探测保留状态与模型声明摘要，不额外留存多轮凭据报文。
		c.exchange.CaptureMode = "summary"
		return c
	}
	m := &CodexTicketHTTPMessage{Method: req.Method}
	if req.URL != nil {
		m.URL = req.URL.String()
	}
	m.Headers, m.HeadersTruncated = codexTicketRawHeaders(req.Header)
	c.exchange.Request = m
	if req.Body == nil || req.Body == http.NoBody {
		return c
	}
	if req.GetBody == nil {
		m.BodyTruncated = true
		return c
	}
	body, err := req.GetBody()
	if err != nil || body == nil {
		m.BodyTruncated = true
		return c
	}
	reader := &codexTicketCaptureBody{ReadCloser: body, message: m}
	_, _ = io.Copy(io.Discard, io.LimitReader(reader, codexTicketCaptureLimit+1))
	_ = reader.Close()
	return c
}

func (c *codexTicketExchangeCapture) captureResponse(resp *http.Response) {
	if c == nil {
		return
	}
	c.exchange.Network = c.network.Snapshot()
	if resp == nil {
		return
	}
	c.exchange.Signals = codexTicketSignalsFromHeaders(resp.Header)
	m := &CodexTicketHTTPMessage{StatusCode: resp.StatusCode}
	if c.exchange.CaptureMode == "summary" {
		c.exchange.Response = m
		return
	}
	m.Headers, m.HeadersTruncated = codexTicketRawHeaders(resp.Header)
	c.exchange.Response = m
	if resp.Body != nil {
		resp.Body = &codexTicketCaptureBody{ReadCloser: resp.Body, message: m}
	}
}

func (c *codexTicketExchangeCapture) setReportedModels(models []string, truncated bool) {
	if c == nil {
		return
	}
	c.exchange.ModelsTruncated = truncated || len(models) > 16
	c.exchange.ReportedModels = make([]string, 0, min(len(models), 16))
	for _, model := range models[:min(len(models), 16)] {
		if len(model) > 160 {
			c.exchange.ModelsTruncated = true
			model = codexTicketUTF8Prefix(model, 160)
		}
		c.exchange.ReportedModels = append(c.exchange.ReportedModels, model)
	}
}

// 总编码大小有界；超过上限时保留当前值能容纳的前缀，并明确标记截断。
func codexTicketRawHeaders(headers http.Header) (map[string][]string, bool) {
	result, keys := make(map[string][]string), make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result[key] = []string{}
		encoded, _ := json.Marshal(result)
		if len(encoded) > codexTicketHeaderLimit {
			delete(result, key)
			return result, true
		}
		for _, value := range headers[key] {
			result[key] = append(result[key], value)
			encoded, _ = json.Marshal(result)
			if len(encoded) <= codexTicketHeaderLimit {
				continue
			}
			last := len(result[key]) - 1
			result[key][last] = ""
			encoded, _ = json.Marshal(result)
			if len(encoded) > codexTicketHeaderLimit {
				result[key] = result[key][:last]
				return result, true
			}
			low, high := 0, min(len(value), codexTicketHeaderLimit)
			for low < high {
				middle := low + (high-low+1)/2
				result[key][last] = codexTicketUTF8Prefix(value, middle)
				encoded, _ = json.Marshal(result)
				if len(encoded) <= codexTicketHeaderLimit {
					low = middle
				} else {
					high = middle - 1
				}
			}
			result[key][last] = codexTicketUTF8Prefix(value, low)
			return result, true
		}
	}
	return result, false
}

func codexTicketUTF8Prefix(value string, limit int) string {
	value = value[:min(len(value), limit)]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

type codexTicketCaptureBody struct {
	io.ReadCloser
	message                     *CodexTicketHTTPMessage
	body                        []byte
	complete, oversized, closed bool
}

func (b *codexTicketCaptureBody) Read(dst []byte) (int, error) {
	n, err := b.ReadCloser.Read(dst)
	b.message.BodyBytes += int64(n)
	if n > codexTicketCaptureLimit-len(b.body) {
		b.oversized = true
	}
	if n > 0 && b.body == nil {
		b.body = make([]byte, 0, codexTicketCaptureLimit)
	}
	b.body = append(b.body, dst[:min(n, codexTicketCaptureLimit-len(b.body))]...)
	if err == io.EOF {
		b.complete = true
	}
	return n, err
}

func (b *codexTicketCaptureBody) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	err := b.ReadCloser.Close()
	b.message.Body = string(b.body)
	b.message.BodyTruncated = !b.complete || b.oversized
	b.body = nil
	return err
}
