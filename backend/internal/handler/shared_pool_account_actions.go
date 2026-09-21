package handler

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

const (
	sharedAccountRefreshCooldown = 10 * time.Second
	sharedAccountUsageReadAction = "usage-read"
)

type sharedAccountActionKey struct {
	id     int64
	action string
}

type sharedAccountActions struct {
	mu     sync.Mutex
	active map[int64]bool
	until  map[sharedAccountActionKey]time.Time
}

// 用量读取可能触发上游补采，与用卡互斥；普通读取不计入刷新冷却。
func (h *SharedPoolHandler) guardSharedAccountAction(c *gin.Context, id int64, action string) (func(), bool) {
	g := &h.actions
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for key, until := range g.until {
		if !until.After(now) {
			delete(g.until, key)
		}
	}
	key := sharedAccountActionKey{id: id, action: action}
	remaining := time.Until(g.until[key])
	if g.active[id] || remaining > 0 {
		seconds := max(1, int(remaining.Seconds())+1)
		c.Header("Retry-After", strconv.Itoa(seconds))
		response.Error(c, http.StatusTooManyRequests, "账号操作进行中或刷新过于频繁，请稍后重试")
		return nil, false
	}
	if g.active == nil {
		g.active = map[int64]bool{}
	}
	if g.until == nil {
		g.until = map[sharedAccountActionKey]time.Time{}
	}
	g.active[id] = true
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		delete(g.active, id)
		if action != sharedAccountUsageReadAction {
			g.until[key] = time.Now().Add(sharedAccountRefreshCooldown)
		}
	}, true
}
