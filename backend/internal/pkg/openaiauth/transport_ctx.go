package openaiauth

import (
	"context"
	"io"
	"net/http"
	"time"
)

// reqContext 基于请求已有 context 派生一个带超时的子 context。
func reqContext(req *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	base := req.Context()
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, timeout)
}

// cancelBody 在 Body 关闭时释放派生 context，避免泄漏。
type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error {
	err := b.ReadCloser.Close()
	if b.cancel != nil {
		b.cancel()
	}
	return err
}
