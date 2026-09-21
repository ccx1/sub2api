package handler

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type sharedOAuthSession struct {
	ownerID  int64
	platform string
	proxyID  *int64
	expires  time.Time
}

type SharedPoolOAuthHandler struct {
	pool        *service.SharedPoolService
	claude      *service.OAuthService
	openai      *service.OpenAIOAuthService
	gemini      *service.GeminiOAuthService
	antigravity *service.AntigravityOAuthService
	mu          sync.Mutex
	sessions    map[string]sharedOAuthSession
	pending     map[int64]int
}

func NewSharedPoolOAuthHandler(pool *service.SharedPoolService, claude *service.OAuthService, openai *service.OpenAIOAuthService,
	gemini *service.GeminiOAuthService, antigravity *service.AntigravityOAuthService) *SharedPoolOAuthHandler {
	return &SharedPoolOAuthHandler{pool: pool, claude: claude, openai: openai, gemini: gemini, antigravity: antigravity, sessions: map[string]sharedOAuthSession{}}
}

func (h *SharedPoolOAuthHandler) Start(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	var input struct {
		ProxyURL  *string `json:"proxy_url"`
		AccountID int64   `json:"account_id"`
	}
	if !sharedBind(c, &input) {
		return
	}
	platform := c.Param("platform")
	if !sharedOAuthPlatform(platform) {
		response.BadRequest(c, "不支持此平台授权")
		return
	}
	h.mu.Lock()
	now := time.Now()
	count := 0
	for id, s := range h.sessions {
		if now.After(s.expires) {
			delete(h.sessions, id)
		} else if s.ownerID == userID {
			count++
		}
	}
	if h.pending == nil {
		h.pending = map[int64]int{}
	}
	totalPending := 0
	for _, n := range h.pending {
		totalPending += n
	}
	full := count+h.pending[userID] >= 5 || len(h.sessions)+totalPending >= 5000
	if !full {
		h.pending[userID]++
	}
	h.mu.Unlock()
	if full {
		response.Error(c, 429, "授权会话过多，请稍后重试")
		return
	}
	defer func() {
		h.mu.Lock()
		h.pending[userID]--
		if h.pending[userID] == 0 {
			delete(h.pending, userID)
		}
		h.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	proxyID, err := h.pool.AccountOAuthProxy(ctx, userID, input.AccountID, platform, input.ProxyURL)
	if err != nil {
		sharedReply(c, nil, err)
		return
	}
	authURL, sessionID, err := h.start(ctx, platform, proxyID)
	if err != nil {
		response.BadRequest(c, "生成授权链接失败，请检查代理或稍后重试")
		return
	}
	h.mu.Lock()
	h.sessions[sessionID] = sharedOAuthSession{userID, platform, proxyID, time.Now().Add(10 * time.Minute)}
	h.mu.Unlock()
	response.Success(c, map[string]string{"auth_url": authURL, "session_id": sessionID})
}

func sharedOAuthPlatform(platform string) bool {
	switch platform {
	case service.PlatformAnthropic, service.PlatformOpenAI, service.PlatformGemini, service.PlatformAntigravity:
		return true
	}
	return false
}

func (h *SharedPoolOAuthHandler) start(ctx context.Context, platform string, proxy *int64) (string, string, error) {
	switch platform {
	case service.PlatformAnthropic:
		v, e := h.claude.GenerateAuthURL(ctx, proxy)
		if e != nil {
			return "", "", e
		}
		return v.AuthURL, v.SessionID, nil
	case service.PlatformOpenAI:
		v, e := h.openai.GenerateAuthURL(ctx, proxy, "", platform)
		if e != nil {
			return "", "", e
		}
		return v.AuthURL, v.SessionID, nil
	case service.PlatformGemini:
		v, e := h.gemini.GenerateAuthURL(ctx, proxy, "", "", "code_assist", "")
		if e != nil {
			return "", "", e
		}
		return v.AuthURL, v.SessionID, nil
	default:
		v, e := h.antigravity.GenerateAuthURL(ctx, proxy)
		if e != nil {
			return "", "", e
		}
		return v.AuthURL, v.SessionID, nil
	}
}

type sharedOAuthFinishInput struct {
	SessionID string `json:"session_id"`
	Code      string `json:"code"`
	State     string `json:"state"`
}

// takeSession 先核验所有者再一次性消费，猜测他人的 session_id 不能使其失效。
func (h *SharedPoolOAuthHandler) takeSession(id, platform string, ownerID int64) (sharedOAuthSession, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[id]
	if !ok || s.ownerID != ownerID || s.platform != platform || time.Now().After(s.expires) {
		return sharedOAuthSession{}, false
	}
	delete(h.sessions, id)
	return s, true
}

func (h *SharedPoolOAuthHandler) Finish(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	var input sharedOAuthFinishInput
	if !sharedBind(c, &input) {
		return
	}
	if input.Code == "" || len(input.Code) > 16384 || len(input.SessionID) > 128 {
		response.BadRequest(c, "授权码格式无效")
		return
	}
	session, ok := h.takeSession(input.SessionID, c.Param("platform"), userID)
	if !ok {
		response.BadRequest(c, "授权会话不存在或已过期，请重新授权")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	credentials, err := h.finish(ctx, session, input)
	if err != nil {
		response.BadRequest(c, "授权失败，请重新生成链接并检查授权码和代理")
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, map[string]any{"credentials": credentials})
}

func (h *SharedPoolOAuthHandler) finish(ctx context.Context, s sharedOAuthSession, in sharedOAuthFinishInput) (map[string]any, error) {
	switch s.platform {
	case service.PlatformAnthropic:
		v, e := h.claude.ExchangeCode(ctx, &service.ExchangeCodeInput{SessionID: in.SessionID, Code: in.Code, ProxyID: s.proxyID})
		if e != nil {
			return nil, e
		}
		raw, e := json.Marshal(v)
		if e != nil {
			return nil, e
		}
		var out map[string]any
		e = json.Unmarshal(raw, &out)
		return out, e
	case service.PlatformOpenAI:
		v, e := h.openai.ExchangeCode(ctx, &service.OpenAIExchangeCodeInput{SessionID: in.SessionID, Code: in.Code, State: in.State, ProxyID: s.proxyID})
		if e != nil {
			return nil, e
		}
		return h.openai.BuildAccountCredentials(v), nil
	case service.PlatformGemini:
		v, e := h.gemini.ExchangeCode(ctx, &service.GeminiExchangeCodeInput{SessionID: in.SessionID, Code: in.Code, State: in.State, ProxyID: s.proxyID, OAuthType: "code_assist"})
		if e != nil {
			return nil, e
		}
		return h.gemini.BuildAccountCredentials(v), nil
	default:
		v, e := h.antigravity.ExchangeCode(ctx, &service.AntigravityExchangeCodeInput{SessionID: in.SessionID, Code: in.Code, State: in.State, ProxyID: s.proxyID})
		if e != nil {
			return nil, e
		}
		return h.antigravity.BuildAccountCredentials(v), nil
	}
}
