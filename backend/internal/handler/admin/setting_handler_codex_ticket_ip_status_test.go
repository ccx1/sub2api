package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexIPStatusReaderStub struct {
	items  []service.CodexIPStatus
	status service.CodexIPStatusKind
	page   int
	size   int
}

func (s *codexIPStatusReaderStub) ListCodexIPStatus(_ context.Context, status service.CodexIPStatusKind, page, pageSize int) ([]service.CodexIPStatus, int64, error) {
	s.status, s.page, s.size = status, page, pageSize
	return s.items, int64(len(s.items)), nil
}

func TestGetCodexTicketIPStatusValidatesAndPaginates(t *testing.T) {
	reader := &codexIPStatusReaderStub{items: []service.CodexIPStatus{{IP: "203.0.113.10", Status: service.CodexIPStatusCooling, UntilAt: func() *time.Time { now := time.Now(); return &now }()}}}
	h := &SettingHandler{codexIPStatusReader: reader}
	router := gin.New()
	router.GET("/status", h.GetCodexTicketIPStatus)
	req := httptest.NewRequest(http.MethodGet, "/status?status=cooling&page=2&page_size=7", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, service.CodexIPStatusCooling, reader.status)
	require.Equal(t, 2, reader.page)
	require.Equal(t, 7, reader.size)

	req = httptest.NewRequest(http.MethodGet, "/status?status=unknown", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
