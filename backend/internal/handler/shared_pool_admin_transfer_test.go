//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type sharedAdminTransferEarningsStub struct {
	service.SharedPoolEarningsRepository
	userID int64
	result *service.SharedPoolEarningsTransfer
	err    error
}

func (s *sharedAdminTransferEarningsStub) Transfer(_ context.Context, userID int64) (*service.SharedPoolEarningsTransfer, error) {
	s.userID = userID
	return s.result, s.err
}

func sharedAdminTransferContext(id string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/shared-pool/users/"+id+"/transfer", nil)
	c.Params = gin.Params{{Key: "id", Value: id}}
	return c, w
}

func TestSharedPoolAdminTransferCreditsRequestedUser(t *testing.T) {
	repo := &sharedAdminTransferEarningsStub{result: &service.SharedPoolEarningsTransfer{ID: 9, Amount: 1.25, Balance: 8.75}}
	h := &SharedPoolHandler{earnings: repo}
	c, w := sharedAdminTransferContext("42")

	h.AdminTransfer(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(42), repo.userID)
	var body struct {
		Data service.SharedPoolEarningsTransfer `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, int64(9), body.Data.ID)
	require.Equal(t, 1.25, body.Data.Amount)
	require.Equal(t, 8.75, body.Data.Balance)
}

func TestSharedPoolAdminTransferRejectsInvalidUserID(t *testing.T) {
	repo := &sharedAdminTransferEarningsStub{result: &service.SharedPoolEarningsTransfer{Amount: 1}}
	h := &SharedPoolHandler{earnings: repo}
	for _, id := range []string{"0", "-1", "invalid"} {
		t.Run(id, func(t *testing.T) {
			c, w := sharedAdminTransferContext(id)
			h.AdminTransfer(c)

			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Zero(t, repo.userID)
		})
	}
}

func TestSharedPoolAdminTransferReturnsRepositoryError(t *testing.T) {
	repo := &sharedAdminTransferEarningsStub{err: service.ErrSharedPoolEarningsEmpty}
	h := &SharedPoolHandler{earnings: repo}
	c, w := sharedAdminTransferContext("42")

	h.AdminTransfer(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, int64(42), repo.userID)
}
