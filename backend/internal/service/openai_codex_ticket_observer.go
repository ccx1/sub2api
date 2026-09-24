package service

import (
	"bytes"
	"encoding/json"
	"strings"
)

// 有界响应观察思路参考 wangyunjeff/sub2api-state-kit，提交 31420ad，LGPL-3.0。
// https://github.com/wangyunjeff/sub2api-state-kit/blob/31420ad/plugin/internal/engine/observer.go
const openAICodexTicketObservedFrameLimit = 1 << 20

type openAICodexTicketResponseObserver struct {
	expected, eventType                       string
	line, event, body                         []byte
	lineOverflow, eventOverflow, bodyOverflow bool
	started, jsonMode, finished               bool
	completed, matches, failed                bool
	responseID                                string
	protocolCompleted                         bool
	failureStatus                             int
	reportedModels                            []string
	modelsTruncated                           bool
	diagnostics                               *codexTicketResponseDiagnostics
	diagnosticEventType                       string
}

func newOpenAICodexTicketResponseObserver(model string) *openAICodexTicketResponseObserver {
	return &openAICodexTicketResponseObserver{expected: model, matches: strings.TrimSpace(model) != ""}
}

// Observe 只读传入字节；SSE 收齐事件即可报告模型不符，不必等待连接关闭。
func (o *openAICodexTicketResponseObserver) Observe(chunk []byte) {
	if o.finished {
		return
	}
	if !o.started {
		trimmed := bytes.TrimLeft(chunk, " \r\n\t")
		if len(trimmed) == 0 {
			if !o.bodyOverflow {
				o.body, o.bodyOverflow = appendOpenAICodexTicketObserved(o.body, chunk)
			}
			return
		}
		o.started, o.jsonMode = true, trimmed[0] == '{'
		if !o.jsonMode {
			o.body, o.bodyOverflow, chunk = nil, false, trimmed
		}
	}
	if o.jsonMode {
		if !o.bodyOverflow {
			o.body, o.bodyOverflow = appendOpenAICodexTicketObserved(o.body, chunk)
		}
		return
	}
	for _, b := range chunk {
		if b == '\n' {
			o.consumeLine()
		} else if !o.lineOverflow {
			o.line, o.lineOverflow = appendOpenAICodexTicketObserved(o.line, []byte{b})
		}
	}
}

// 除长度外也限制容量，避免 Go 切片扩容使每个观察缓冲超过约定上限。
func appendOpenAICodexTicketObserved(dst, chunk []byte) ([]byte, bool) {
	if len(chunk) > openAICodexTicketObservedFrameLimit-len(dst) {
		return nil, true
	}
	size := len(dst) + len(chunk)
	if size > cap(dst) {
		capacity := min(openAICodexTicketObservedFrameLimit, max(size, max(256, cap(dst)*2)))
		grown := make([]byte, len(dst), capacity)
		copy(grown, dst)
		dst = grown
	}
	return append(dst, chunk...), false
}

func (o *openAICodexTicketResponseObserver) consumeLine() {
	if o.lineOverflow {
		o.eventOverflow, o.lineOverflow = true, false
		o.line = nil
		return
	}
	line := bytes.TrimSuffix(o.line, []byte{'\r'})
	if len(line) == 0 {
		if !o.eventOverflow && len(o.event) > 0 {
			o.inspect(o.event)
		}
		if o.diagnostics != nil && o.eventOverflow {
			o.diagnostics.declaration.Truncated = true
		}
		o.diagnosticEventType = ""
		o.event, o.eventOverflow, o.eventType = o.event[:0], false, ""
	} else if bytes.HasPrefix(line, []byte("data:")) && !o.eventOverflow {
		data := bytes.TrimPrefix(line[5:], []byte{' '})
		o.event, o.eventOverflow = appendOpenAICodexTicketObserved(o.event, data)
		if !o.eventOverflow {
			o.event, o.eventOverflow = appendOpenAICodexTicketObserved(o.event, []byte{'\n'})
		}
	} else if bytes.HasPrefix(line, []byte("event:")) {
		name := bytes.TrimPrefix(line[6:], []byte{' '})
		if o.diagnostics != nil {
			o.diagnosticEventType = codexTicketUTF8Prefix(string(name), 96)
		}
		o.eventType = "other"
		if bytes.Equal(name, []byte("response.completed")) {
			o.eventType = "response.completed"
		} else if bytes.Equal(name, []byte("error")) || bytes.Equal(name, []byte("response.failed")) || bytes.Equal(name, []byte("response.incomplete")) {
			o.failed = true
		}
	}
	o.line = o.line[:0]
}

func (o *openAICodexTicketResponseObserver) Finish() {
	if o.finished {
		return
	}
	o.finished = true
	if o.jsonMode && !o.bodyOverflow {
		o.inspect(o.body)
	}
	if o.diagnostics != nil && (o.bodyOverflow || o.lineOverflow || o.eventOverflow || len(o.line) > 0 || len(o.event) > 0) {
		o.diagnostics.declaration.Truncated = true
	}
	// SSE 在空行处派发；EOF 不能把缺少事件终止符的片段变成成功响应。
	o.line, o.event, o.body = nil, nil, nil
}

func (o *openAICodexTicketResponseObserver) Result() (completed bool, matches bool) {
	return o.completed && !o.failed, o.completed && !o.failed && o.matches
}

func (o *openAICodexTicketResponseObserver) inspect(data []byte) {
	var value struct {
		Type     string          `json:"type"`
		Object   string          `json:"object"`
		ID       string          `json:"id"`
		Status   string          `json:"status"`
		Model    string          `json:"model"`
		Error    json.RawMessage `json:"error"`
		Response *struct {
			ID     string          `json:"id"`
			Status string          `json:"status"`
			Model  string          `json:"model"`
			Error  json.RawMessage `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(data, &value) != nil {
		return
	}
	if o.diagnostics != nil {
		o.diagnostics.observe(data, o.diagnosticEventType, o.jsonMode)
	}
	if openAICodexTicketResponseFailed(value.Type, value.Status, value.Error) || (value.Response != nil &&
		openAICodexTicketResponseFailed("", value.Response.Status, value.Response.Error)) {
		o.failed = true
		if status := openAIWSPayloadTransientStatus(data); status >= 500 {
			o.failureStatus = status
		}
		return
	}
	model := value.Model
	if o.jsonMode {
		if value.Object != "response" || value.Status != "completed" || (value.Type != "" && value.Type != "response.completed") {
			return
		}
	} else {
		kind := value.Type
		if kind == "" {
			kind = o.eventType
		}
		if kind != "response.completed" || (o.eventType != "" && o.eventType != kind) || value.Response == nil ||
			(value.Status != "" && value.Status != "completed") || !openAICodexTicketNoResponseError(value.Response.Error) ||
			(value.Response.Status != "" && value.Response.Status != "completed") {
			return
		}
		model = value.Response.Model
	}
	o.protocolCompleted = true
	if strings.TrimSpace(model) == "" {
		return
	}
	o.completed = true
	if value.Response != nil {
		o.responseID = strings.TrimSpace(value.Response.ID)
	} else {
		o.responseID = strings.TrimSpace(value.ID)
	}
	o.matches = o.matches && model == o.expected
	o.recordReportedModel(model)
}

func (o *openAICodexTicketResponseObserver) recordReportedModel(model string) {
	for _, recorded := range o.reportedModels {
		if recorded == model {
			return
		}
	}
	// 诊断数据不能随异常上游响应无限增长；模型匹配仍使用未经截断的原值。
	if len(o.reportedModels) >= 8 || len(model) > 256 {
		o.modelsTruncated = true
		return
	}
	o.reportedModels = append(o.reportedModels, model)
}

func openAICodexTicketNoResponseError(raw json.RawMessage) bool {
	return len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func openAICodexTicketResponseFailed(kind, status string, responseError json.RawMessage) bool {
	return kind == "error" || kind == "response.failed" || kind == "response.incomplete" ||
		status == "failed" || status == "incomplete" || !openAICodexTicketNoResponseError(responseError)
}
