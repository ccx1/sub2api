package service

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

// 只在新一轮请求写入成功后记录出口，存量连接不能绕过已修改的代理池策略。
type randomProxyObservedWSFrameConn struct {
	openaiwsv2.FrameConn
	account            *Account
	source             any
	routingInvalidated atomic.Bool
	failureOnce        sync.Once
	modelMu            sync.Mutex
	model              string
	resultReported     bool
}

func (c *randomProxyObservedWSFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	typ, payload, err := c.FrameConn.ReadFrame(ctx)
	if err != nil && !c.routingInvalidated.Load() {
		c.reportTransportFailure(ctx, err)
	}
	if err == nil {
		kind := gjson.GetBytes(payload, "type").String()
		terminal := kind == "response.completed" || kind == "response.failed" || kind == "response.incomplete" || kind == "error"
		c.modelMu.Lock()
		model := c.model
		report := terminal && !c.resultReported
		if terminal {
			c.resultReported = true
		}
		c.modelMu.Unlock()
		if report {
			observeRandomProxyWSTerminal(ctx, c.account, c.source, model, payload)
		}
	}
	return typ, payload, err
}

func (c *randomProxyObservedWSFrameConn) WriteFrame(ctx context.Context, typ coderws.MessageType, payload []byte) error {
	isRequest := (typ == coderws.MessageText || typ == coderws.MessageBinary) && gjson.GetBytes(payload, "type").String() == "response.create"
	if isRequest {
		c.modelMu.Lock()
		c.model = gjson.GetBytes(payload, "model").String()
		c.resultReported = false
		c.modelMu.Unlock()
		if err := ValidateRandomProxyForReuse(ctx, c.account, c.source); err != nil {
			// 策略变化主动关闭旧连接，读协程产生的关闭错误不算代理故障。
			c.routingInvalidated.Store(true)
			_ = c.FrameConn.Close()
			return err
		}
	}
	if err := c.FrameConn.WriteFrame(ctx, typ, payload); err != nil {
		c.reportTransportFailure(ctx, err)
		return err
	}
	if isRequest {
		RecordRandomProxyUsage(ctx, c.account, c.source)
	}
	return nil
}

func (c *randomProxyObservedWSFrameConn) reportTransportFailure(ctx context.Context, err error) {
	c.failureOnce.Do(func() {
		c.modelMu.Lock()
		reported := c.resultReported
		c.resultReported = true
		c.modelMu.Unlock()
		if !reported {
			reportRandomProxyWSFailure(ctx, c.account, c.source, err)
		}
	})
}

func observeRandomProxyWSTerminal(ctx context.Context, account *Account, source any, model string, payload []byte) {
	if account == nil || !account.IsRandomProxy() {
		return
	}
	if openAIWSPayloadTransientStatus(payload) >= 500 {
		reportRandomProxyResult(ctx, account, source, false)
		return
	}
	observer := newOpenAICodexTicketResponseObserver(model)
	observer.inspect(payload)
	if complete, matches := observer.Result(); complete {
		reportRandomProxyResult(ctx, account, source, model == "" || matches)
	}
}

func reportRandomProxyWSFailure(ctx context.Context, account *Account, source any, err error) bool {
	// 对端显式关闭包含正常退出、限流和策略拒绝，不能据此淘汰代理。
	status := coderws.CloseStatus(err)
	if status != -1 && status != coderws.StatusAbnormalClosure {
		return false
	}
	if status == coderws.StatusAbnormalClosure {
		err = errors.Join(err, io.ErrUnexpectedEOF)
	}
	return ReportRandomProxyTransportFailure(ctx, account, source, err)
}
