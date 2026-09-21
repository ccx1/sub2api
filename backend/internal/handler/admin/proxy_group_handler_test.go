package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type proxyGroupAdminStub struct {
	service.AdminService
	service.ProxyGroupAdminService
	update      *service.UpdateProxyInput
	create      *service.CreateProxyInput
	filter      int64
	filtered    bool
	assignGroup *int64
	assignIDs   []int64
}

func (s *proxyGroupAdminStub) UpdateProxy(_ context.Context, _ int64, in *service.UpdateProxyInput) (*service.Proxy, error) {
	s.update = in
	return &service.Proxy{ID: 1, GroupID: in.GroupID}, nil
}
func (s *proxyGroupAdminStub) CreateProxy(_ context.Context, in *service.CreateProxyInput) (*service.Proxy, error) {
	s.create = in
	return &service.Proxy{ID: 1, GroupID: in.GroupID}, nil
}
func (s *proxyGroupAdminStub) ListProxiesWithAccountCount(ctx context.Context, _, _ int, _, _, _, _, _ string) ([]service.ProxyWithAccountCount, int64, error) {
	s.filter, s.filtered = service.ProxyGroupFilterFromContext(ctx)
	return []service.ProxyWithAccountCount{}, 0, nil
}
func (s *proxyGroupAdminStub) AssignProxyGroup(_ context.Context, ids []int64, group *int64) (int64, error) {
	s.assignIDs, s.assignGroup = ids, group
	return int64(len(ids)), nil
}

func TestProxyGroupUpdatePreservesOmittedAndClearsNull(t *testing.T) {
	for _, test := range []struct {
		body  string
		group *int64
		clear bool
	}{
		{body: `{"name":"changed"}`},
		{body: `{"group_id":null}`, clear: true},
		{body: `{"group_id":3}`, group: func() *int64 { v := int64(3); return &v }()},
	} {
		s := &proxyGroupAdminStub{}
		router := gin.New()
		router.PUT("/proxies/:id", NewProxyHandler(s).Update)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/proxies/1", strings.NewReader(test.body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, test.group, s.update.GroupID)
		require.Equal(t, test.clear, s.update.ClearGroupID)
	}
}

func TestProxyGroupListFilterValidation(t *testing.T) {
	for _, test := range []struct {
		query    string
		status   int
		filtered bool
		group    int64
	}{
		{"", 200, false, 0}, {"?group_id=0", 200, true, 0}, {"?group_id=5", 200, true, 5}, {"?group_id=-1", 400, false, 0}, {"?group_id=abc", 400, false, 0},
	} {
		s := &proxyGroupAdminStub{}
		router := gin.New()
		router.GET("/proxies", NewProxyHandler(s).List)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/proxies"+test.query, nil))
		require.Equal(t, test.status, w.Code, w.Body.String())
		require.Equal(t, test.filtered, s.filtered)
		require.Equal(t, test.group, s.filter)
	}
}

func TestProxyGroupBatchRequiresExplicitTarget(t *testing.T) {
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"ids":[1,2],"group_id":null}`, 200},
		{`{"ids":[1,2],"group_id":3}`, 200},
		{`{"ids":[1,2]}`, 400},
		{`{"ids":[0],"group_id":3}`, 400},
		{`{"ids":[],"group_id":3}`, 400},
	} {
		s := &proxyGroupAdminStub{}
		router := gin.New()
		router.POST("/proxies/batch-group", NewProxyHandler(s).BatchGroup)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/proxies/batch-group", strings.NewReader(test.body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		require.Equal(t, test.status, w.Code, w.Body.String())
		if test.status == 200 {
			require.Equal(t, []int64{1, 2}, s.assignIDs)
			require.Contains(t, w.Body.String(), `"updated_count":2`)
		}
	}
}
