package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/securitypolicykeyword"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSecurityPolicyListKeywordsWithRuntimeInterceptors(t *testing.T) {
	for _, count := range []int{0, 1} {
		t.Run(map[int]string{0: "empty", 1: "with disabled keyword"}[count], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := repository.NewSecurityPolicyRepository(client, db)
			svc := service.NewSecurityPolicyService(nil, repo, nil, nil, nil, nil, nil)
			handler := NewSecurityPolicyHandler(svc)
			rows := sqlmock.NewRows(securitypolicykeyword.Columns)
			if count > 0 {
				now := time.Now()
				rows.AddRow(int64(1), now, now, nil, nil, "test-keyword", "custom", false)
			}
			mock.ExpectQuery(`SELECT .* FROM "security_policy_keywords" WHERE .*deleted_at.*IS NULL.*ORDER BY.*LIMIT 10000`).
				WillReturnRows(rows)
			router := gin.New()
			router.GET("/api/v1/admin/security-policy/keywords", handler.ListKeywords)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
				"/api/v1/admin/security-policy/keywords?include_disabled=true", nil))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var body struct {
				Code int `json:"code"`
				Data struct {
					Keywords []dto.SecurityPolicyKeyword `json:"keywords"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Zero(t, body.Code)
			require.NotNil(t, body.Data.Keywords)
			require.Len(t, body.Data.Keywords, count)
			if count > 0 {
				require.Equal(t, "test-keyword", body.Data.Keywords[0].Keyword)
				require.False(t, body.Data.Keywords[0].Enabled)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
