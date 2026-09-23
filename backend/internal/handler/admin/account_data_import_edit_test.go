package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type importEditAccountStore struct{ *stubAdminService }

func (s *importEditAccountStore) CreateAccount(ctx context.Context, in *service.CreateAccountInput) (*service.Account, error) {
	if in.Name == "create-fails" {
		return nil, errors.New("creation failed")
	}
	account, err := s.stubAdminService.CreateAccount(ctx, in)
	if err == nil {
		account.ID += int64(len(s.createdAccounts))
	}
	return account, err
}

func TestImportDataReturnsOnlySuccessfullyCreatedAccountIDs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accounts []DataAccount
		wantIDs  []int64
		failed   int
	}{
		{"single", []DataAccount{importEditDataAccount("same-name")}, []int64{301}, 0},
		{"multiple_same_name", []DataAccount{importEditDataAccount("same-name"), importEditDataAccount("same-name")}, []int64{301, 302}, 0},
		{"partial_failure", []DataAccount{importEditDataAccount("same-name"), {}, importEditDataAccount("create-fails"), importEditDataAccount("same-name")}, []int64{301, 302}, 2},
		{"all_failed", []DataAccount{{}, importEditDataAccount("create-fails")}, []int64{}, 2},
		{"no_accounts", []DataAccount{}, []int64{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			store := &importEditAccountStore{newStubAdminService()}
			h := &AccountHandler{adminService: store}
			router := gin.New()
			router.POST("/api/v1/admin/accounts/data", h.ImportData)
			body, err := json.Marshal(DataImportRequest{Data: DataPayload{Accounts: tc.accounts, Proxies: []DataProxy{}}})
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var response struct {
				Data struct {
					AccountIDs     []int64 `json:"account_ids"`
					AccountCreated int     `json:"account_created"`
					AccountFailed  int     `json:"account_failed"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Equal(t, tc.wantIDs, response.Data.AccountIDs)
			require.Equal(t, len(tc.wantIDs), response.Data.AccountCreated)
			require.Equal(t, tc.failed, response.Data.AccountFailed)
		})
	}
}

func importEditDataAccount(name string) DataAccount {
	return DataAccount{Name: name, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test-import-token"}, Concurrency: 1}
}
