package service

import (
	"context"
	"errors"
	"io"
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
}

func (c *randomProxyObservedWSFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	typ, payload, err := c.FrameConn.ReadFrame(ctx)
	if err != nil && !c.routingInvalidated.Load() {
		reportRandomProxyWSFailure(ctx, c.account, c.source, err)
	}
	return typ, payload, err
}

func (c *randomProxyObservedWSFrameConn) WriteFrame(ctx context.Context, typ coderws.MessageType, payload []byte) error {
	isRequest := (typ == coderws.MessageText || typ == coderws.MessageBinary) && gjson.GetBytes(payload, "type").String() == "response.create"
	if isRequest {
		if err := ValidateRandomProxyForReuse(ctx, c.account, c.source); err != nil {
			// 策略变化主动关闭旧连接，读协程产生的关闭错误不算代理故障。
			c.routingInvalidated.Store(true)
			_ = c.FrameConn.Close()
			return err
		}
	}
	if err := c.FrameConn.WriteFrame(ctx, typ, payload); err != nil {
		reportRandomProxyWSFailure(ctx, c.account, c.source, err)
		return err
	}
	if isRequest {
		RecordRandomProxyUsage(ctx, c.account, c.source)
	}
	return nil
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
