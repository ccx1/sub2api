package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type autoTransferHandlerRepo struct {
	service.SharedPoolAutoTransferRepository
	userID int64
	saved  service.SharedPoolAutoTransferUpdate
}

func (r *autoTransferHandlerRepo) Get(_ context.Context, id int64) (*service.SharedPoolAutoTransferSettings, error) {
	r.userID = id
	return &service.SharedPoolAutoTransferSettings{Threshold: 1, DailyTime: "00:00", Timezone: "Asia/Shanghai"}, nil
}

func (r *autoTransferHandlerRepo) Save(_ context.Context, id int64, input service.SharedPoolAutoTransferUpdate) (*service.SharedPoolAutoTransferSettings, error) {
	r.userID, r.saved = id, input
	return &service.SharedPoolAutoTransferSettings{Enabled: input.Enabled, Threshold: input.Threshold, DailyTime: input.DailyTime, Timezone: "Asia/Shanghai"}, nil
}

func autoTransferContext(method, body string, userID int64) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/api/v1/shared-pool/auto-transfer", strings.NewReader(body))
	if userID > 0 {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
	}
	return c, w
}

func TestSharedPoolAutoTransferHandlerRequiresIdentity(t *testing.T) {
	h := &SharedPoolHandler{}
	for _, call := range []func(*gin.Context){h.AutoTransferSettings, h.SaveAutoTransferSettings} {
		c, w := autoTransferContext(http.MethodGet, "", 0)
		call(c)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
}

func TestSharedPoolAutoTransferHandlerOwnSettings(t *testing.T) {
	repo := &autoTransferHandlerRepo{}
	svc := service.NewSharedPoolAutoTransferService(repo, nil, nil, nil, nil)
	defer svc.Stop()
	h := &SharedPoolHandler{autoTransfer: svc}
	c, w := autoTransferContext(http.MethodGet, "", 7)
	h.AutoTransferSettings(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(7), repo.userID)
	c, w = autoTransferContext(http.MethodPut, `{"enabled":true,"threshold":2.5,"daily_time":"09:30"}`, 8)
	h.SaveAutoTransferSettings(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(8), repo.userID)
	require.Equal(t, service.SharedPoolAutoTransferUpdate{Enabled: true, Threshold: 2.5, DailyTime: "09:30"}, repo.saved)
	var body struct {
		Data service.SharedPoolAutoTransferSettings
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Data.Enabled)
	require.Equal(t, "Asia/Shanghai", body.Data.Timezone)
}

func TestSharedPoolAutoTransferHandlerRejectsInvalidOrForeignFields(t *testing.T) {
	for _, body := range []string{
		`{"threshold":1,"daily_time":"00:00"}`,
		`{"enabled":null,"threshold":1,"daily_time":"00:00"}`,
		`{"enabled":true,"threshold":0,"daily_time":"00:00"}`,
		`{"enabled":true,"threshold":1,"daily_time":"24:00"}`,
		`{"enabled":true,"threshold":1,"daily_time":"00:00","user_id":9}`,
		`{"enabled":true,"threshold":1,"daily_time":"00:00","timezone":"UTC"}`,
		`{"enabled":true,"threshold":1,"daily_time":"00:00","last_run_date":"2026-01-01"}`,
		`{"enabled":false,"threshold":1,"daily_time":"00:00"}{}`,
	} {
		t.Run(body, func(t *testing.T) {
			repo := &autoTransferHandlerRepo{}
			svc := service.NewSharedPoolAutoTransferService(repo, nil, nil, nil, nil)
			defer svc.Stop()
			h := &SharedPoolHandler{autoTransfer: svc}
			c, w := autoTransferContext(http.MethodPut, body, 7)
			h.SaveAutoTransferSettings(c)
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Zero(t, repo.userID)
		})
	}
}
