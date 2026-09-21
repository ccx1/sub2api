package service

import (
	"context"
	"crypto/sha256"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

// 只捕获请求握手实际携带的原生票据，不能从响应头或后续缓存反推。
type openAICodexTicketWSReceipt struct {
	account *Account
	ticket  openAICodexTicket
	config  config.OpenAICodexTicketConfig
	once    sync.Once
}

func codexTicketWSReceiptFromSnapshot(receipt *openAICodexTicketReceipt) *openAICodexTicketWSReceipt {
	if receipt == nil {
		return nil
	}
	return &openAICodexTicketWSReceipt{account: receipt.account, ticket: receipt.ticket, config: receipt.config}
}

func (r *openAICodexTicketWSReceipt) invalidate(ctx context.Context, s *OpenAIGatewayService) {
	if r != nil {
		r.once.Do(func() { go s.invalidateOpenAICodexTicket(ctx, r.account, &r.ticket) })
	}
}

func (r *openAICodexTicketWSReceipt) observeHandshake(ctx context.Context, s *OpenAIGatewayService, headers http.Header) {
	state := extractOpenAICodexTurnState(headers)
	if r != nil && codexTicketStateRejected(state, config.NormalizeOpenAICodexTicketConfig(r.config)) {
		r.invalidate(ctx, s)
	}
}

func (r *openAICodexTicketWSReceipt) watch(ctx context.Context, s *OpenAIGatewayService, model string) *openAICodexTicketWSWatchdog {
	if r == nil || model != r.ticket.Model {
		return nil
	}
	return &openAICodexTicketWSWatchdog{observer: newOpenAICodexTicketResponseObserver(model), invalidate: func() { r.invalidate(ctx, s) }}
}

type openAICodexTicketWSWatchdog struct {
	observer   *openAICodexTicketResponseObserver
	invalidate func()
}

func (w *openAICodexTicketWSWatchdog) observe(payload []byte) {
	if w == nil || len(payload) > openAICodexTicketObservedFrameLimit {
		return
	}
	// WS 消息已有完整 JSON 边界，复用事件判定且不改写传输字节。
	w.observer.inspect(payload)
	if completed, matches := w.observer.Result(); completed && !matches {
		w.invalidate()
	}
}

// 使用连接实际发送的快照，复用时不能用本轮准备的请求头反推握手票据。
func confirmedOpenAICodexTicketWSReceipt(_ *openAICodexTicketWSReceipt, lease *openAIWSConnLease) *openAICodexTicketWSReceipt {
	if lease == nil || lease.conn == nil {
		return nil
	}
	return lease.conn.codexTicketReceipt
}

type openAICodexTicketWSIdentity struct {
	state      [32]byte
	model      string
	capturedAt int64
}

func (r *openAICodexTicketWSReceipt) identity() openAICodexTicketWSIdentity {
	if r == nil {
		return openAICodexTicketWSIdentity{}
	}
	return openAICodexTicketWSIdentity{state: sha256.Sum256([]byte(r.ticket.State)),
		model: r.ticket.Model, capturedAt: r.ticket.CapturedAt.UnixNano()}
}

func normalizeOpenAIWSTicketCompatibility(req openAIWSAcquireRequest) openAIWSHandshakeCompatibilityKey {
	key := normalizeOpenAIWSTransportCompatibility(req)
	// 没有原生注入凭证时，不把客户端 STATE 当作受管理票据。
	if receipt := req.CodexTicketReceipt; receipt != nil && req.Headers.Get(openAICodexTurnStateHeader) == receipt.ticket.State {
		key.codexTicket = receipt.identity()
	}
	return key
}

func (s *OpenAIGatewayService) refreshOpenAICodexTicketWSHeaders(ctx context.Context, account *Account, model string, req *openAIWSAcquireRequest) error {
	req.Headers = cloneHeader(req.Headers)
	if req.Headers == nil {
		req.Headers = make(http.Header)
	}
	if old := req.CodexTicketReceipt; old != nil && req.Headers.Get(openAICodexTurnStateHeader) == old.ticket.State {
		req.Headers.Del(openAICodexTurnStateHeader)
	}
	account, err := s.refreshOpenAICodexTicketWSAccount(ctx, account)
	if err != nil {
		return err
	}
	receipt, err := s.applyOpenAICodexTicketSnapshot(ctx, account, model, req.Headers)
	if err != nil {
		return err
	}
	req.CodexTicketReceipt = codexTicketWSReceiptFromSnapshot(receipt)
	return nil
}

// refreshOpenAICodexTicketWSHeadersForTurn keeps the snapshot captured while
// constructing the initial handshake. A first-attempt refresh could observe a
// different policy or cached ticket after the header was injected, leaving the
// dial request and its receipt out of sync. Later turns and explicit retries
// are allowed to refresh both together.
func (s *OpenAIGatewayService) refreshOpenAICodexTicketWSHeadersForTurn(
	ctx context.Context,
	account *Account,
	model string,
	turn int,
	retry int,
	req *openAIWSAcquireRequest,
) error {
	if turn <= 1 && retry == 0 {
		return nil
	}
	return s.refreshOpenAICodexTicketWSHeaders(ctx, account, model, req)
}

func (s *OpenAIGatewayService) refreshOpenAICodexTicketWSAccount(ctx context.Context, account *Account) (*Account, error) {
	if s == nil || !isOpenAICodexTicketAccount(account) {
		return account, nil
	}
	repo, ok := s.accountRepo.(codexTicketAccountReloader)
	if !ok {
		return account, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	latest, err := repo.GetCodexTicketAccountSnapshot(ctx, account.ID)
	if err != nil || latest == nil || latest.ID != account.ID || latest.Status != StatusActive ||
		OpenAICodexTicketAccountEnabled(latest) != OpenAICodexTicketAccountEnabled(account) ||
		openAICodexTicketAccountBinding(latest) != openAICodexTicketAccountBinding(account) {
		// 只刷新票据快照不能改变既有 WS 出口；配置变化必须让客户端重新连接。
		return nil, ErrOpenAICodexTicketUnavailable
	}
	return latest, nil
}

type openAICodexTicketWSFrameConn struct {
	openaiwsv2.FrameConn
	mu                  sync.Mutex
	ctx                 context.Context
	service             *OpenAIGatewayService
	account             *Account
	receipt             *openAICodexTicketWSReceipt
	watchdog            *openAICodexTicketWSWatchdog
	inFlight            bool
	observationDisabled bool
}

func (s *OpenAIGatewayService) observeOpenAICodexTicketWSFrames(ctx context.Context, conn openaiwsv2.FrameConn, receipt *openAICodexTicketWSReceipt, accounts ...*Account) openaiwsv2.FrameConn {
	var account *Account
	if receipt != nil {
		account = receipt.account
	} else if len(accounts) > 0 && OpenAICodexTicketAccountEnabled(accounts[0]) {
		account = cloneOpenAICodexTicketAccount(accounts[0])
	}
	if account == nil {
		return conn
	}
	return &openAICodexTicketWSFrameConn{FrameConn: conn, ctx: ctx, service: s, account: account, receipt: receipt}
}

func (c *openAICodexTicketWSFrameConn) WriteFrame(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	if (kind == coderws.MessageText || kind == coderws.MessageBinary) && gjson.GetBytes(payload, "type").String() == "response.create" {
		c.mu.Lock()
		model := gjson.GetBytes(payload, "model").String()
		req := openAIWSAcquireRequest{}
		err := c.service.refreshOpenAICodexTicketWSHeaders(ctx, c.account, model, &req)
		if err != nil || c.receipt.identity() != req.CodexTicketReceipt.identity() {
			c.mu.Unlock()
			return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation,
				"upstream continuation connection is unavailable; please restart the conversation", ErrOpenAICodexTicketUnavailable)
		}
		if c.inFlight {
			// 无法关联并行响应时停止观察，绝不让后一请求覆盖前一请求快照。
			c.watchdog, c.observationDisabled = nil, true
		} else if !c.observationDisabled {
			c.watchdog = c.receipt.watch(c.ctx, c.service, model)
		}
		c.inFlight = true
		c.mu.Unlock()
	}
	return c.FrameConn.WriteFrame(ctx, kind, payload)
}

func (c *openAICodexTicketWSFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	kind, payload, err := c.FrameConn.ReadFrame(ctx)
	if err == nil && (kind == coderws.MessageText || kind == coderws.MessageBinary) {
		c.mu.Lock()
		c.watchdog.observe(payload)
		if openAIWSPassthroughIsTerminalOutput(payload) {
			c.watchdog, c.inFlight = nil, false
		}
		c.mu.Unlock()
	}
	return kind, payload, err
}
