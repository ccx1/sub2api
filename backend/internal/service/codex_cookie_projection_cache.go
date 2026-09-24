package service

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const maxCodexCookieProjectionProofs = 512

// 缓存只保存脱敏键和验证截止时间，不保存 Cookie 或授权头。
type codexCookieProjectionCache struct {
	mu      sync.Mutex
	proofs  map[string]time.Time
	flights singleflight.Group
}

func (c *codexCookieProjectionCache) verified(key string, now time.Time) bool {
	key = hashCodexCookieProjectionKey(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	expires, ok := c.proofs[key]
	if ok && !expires.After(now) {
		delete(c.proofs, key)
		return false
	}
	return ok
}

func (c *codexCookieProjectionCache) remember(key string, expires time.Time) {
	key = hashCodexCookieProjectionKey(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proofs == nil {
		c.proofs = make(map[string]time.Time)
	}
	now := time.Now()
	var oldest string
	for existing, deadline := range c.proofs {
		if !deadline.After(now) {
			delete(c.proofs, existing)
			continue
		}
		if oldest == "" || deadline.Before(c.proofs[oldest]) {
			oldest = existing
		}
	}
	if len(c.proofs) >= maxCodexCookieProjectionProofs {
		delete(c.proofs, oldest)
	}
	if expires.After(now) {
		c.proofs[key] = expires
	}
}

func hashCodexCookieProjectionKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
