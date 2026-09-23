package service

import (
	"crypto/sha256"
	"strings"

	"github.com/tidwall/gjson"
)

type codexTicketResponseDiagnostics struct {
	declaration   CodexTicketModelDeclaration
	firstHash     [32]byte
	hasFirst      bool
	upstreamError *CodexTicketUpstreamError
	signals       *CodexTicketSignals
	wireStatus    int
}

// 摘要独立于成功判定；首声明不能替代完整终态的真实模型。
func (d *codexTicketResponseDiagnostics) observe(data []byte, event string, jsonMode bool) {
	root := gjson.ParseBytes(data)
	kind := root.Get("type").String()
	if kind == "" {
		kind = event
	}
	if jsonMode && root.Get("object").String() == "response" {
		kind = "response." + root.Get("status").String()
	}
	if codexTicketModelEvent(kind) {
		model := root.Get("response.model")
		if jsonMode && root.Get("object").String() == "response" {
			model = root.Get("model")
		}
		d.observeModel(kind, model)
	}
	if kind == "error" || kind == "response.failed" || root.Get("error").Exists() || root.Get("response.error").Exists() {
		if detail := classifyCodexTicketUpstreamError(data, d.wireStatus, ""); detail != nil {
			if d.upstreamError == nil || detail.Scope == "credential" {
				d.upstreamError = detail
			}
		}
	}
	d.signals = observeCodexTicketSignals(d.signals, root, kind)
}

func codexTicketModelEvent(kind string) bool {
	switch kind {
	case "response.created", "response.in_progress", "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

func (d *codexTicketResponseDiagnostics) observeModel(kind string, model gjson.Result) {
	terminal := kind != "response.created" && kind != "response.in_progress"
	if terminal {
		d.declaration.TerminalEvent = kind
		d.declaration.TerminalModel = ""
	}
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return
	}
	value := model.String()
	hash := sha256.Sum256([]byte(value))
	shown := codexTicketUTF8Prefix(value, 160)
	d.declaration.Truncated = d.declaration.Truncated || len(value) > 160
	if !d.hasFirst {
		d.firstHash, d.hasFirst = hash, true
		d.declaration.FirstModel, d.declaration.FirstEvent = shown, kind
	}
	if terminal {
		d.declaration.TerminalModel = shown
		conflict := hash != d.firstHash || d.declaration.Conflict != nil && *d.declaration.Conflict
		d.declaration.Conflict = &conflict
	}
}

func (c *codexTicketExchangeCapture) captureObserver(observer *openAICodexTicketResponseObserver) {
	if c == nil {
		return
	}
	c.setReportedModels(observer.reportedModels, observer.modelsTruncated)
	d := observer.diagnostics
	if d == nil {
		return
	}
	if d.hasFirst || d.declaration.TerminalEvent != "" || d.declaration.Truncated {
		copy := d.declaration
		c.exchange.ModelDeclaration = &copy
	}
	c.exchange.UpstreamError, c.exchange.Signals = d.upstreamError, d.signals
}
