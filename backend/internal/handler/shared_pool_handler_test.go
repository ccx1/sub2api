//go:build unit

package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolStrictInputRejectsAdminFields(t *testing.T) {
	for _, body := range []string{
		`{"name":"mine","group_ids":[1]}`, `{"name":"mine","rate_multiplier":0}`,
		`{"name":"mine","settlement_multiplier":0}`,
		`{"name":"mine","owner_user_id":99}`, `{"name":"mine","extra":{"shared_pool_owner_id":99}}`,
		`{"name":"mine"} {}`, strings.Repeat(" ", 128<<10) + `{}`,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
		require.False(t, sharedBind(c, &service.SharedPoolAccountInput{}))
		require.Equal(t, 400, w.Code)
	}
}

func TestSharedPoolOAuthSessionOwnershipAndOneTimeUse(t *testing.T) {
	h := &SharedPoolOAuthHandler{sessions: map[string]sharedOAuthSession{"test": {ownerID: 7, platform: "openai", expires: time.Now().Add(time.Minute)}}}
	_, ok := h.takeSession("test", "openai", 8)
	require.False(t, ok)
	_, ok = h.takeSession("test", "gemini", 7)
	require.False(t, ok)
	_, ok = h.takeSession("test", "openai", 7)
	require.True(t, ok)
	_, ok = h.takeSession("test", "openai", 7)
	require.False(t, ok)
	h.sessions["expired"] = sharedOAuthSession{ownerID: 7, platform: "openai", expires: time.Now().Add(-time.Minute)}
	_, ok = h.takeSession("expired", "openai", 7)
	require.False(t, ok)
}

func TestSharedPoolMissingIdentityFailsClosed(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	id, ok := sharedUser(c)
	require.False(t, ok)
	require.Zero(t, id)
	require.Equal(t, 401, w.Code)
}

func TestSharedPoolOAuthCountsInFlightSessions(t *testing.T) {
	h := &SharedPoolOAuthHandler{sessions: map[string]sharedOAuthSession{}, pending: map[int64]int{7: 5}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/oauth/openai/start", strings.NewReader(`{}`))
	c.Params = gin.Params{{Key: "platform", Value: "openai"}}
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	h.Start(c)
	require.Equal(t, 429, w.Code)
	require.Equal(t, 5, h.pending[7])
}
