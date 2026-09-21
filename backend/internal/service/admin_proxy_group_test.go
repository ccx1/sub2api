package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type proxyGroupRepoStub struct {
	ProxyRepository
	proxy    *Proxy
	groups   []ProxyGroup
	assigned []int64
	groupID  *int64
}

func (r *proxyGroupRepoStub) GetByID(context.Context, int64) (*Proxy, error) { return r.proxy, nil }
func (r *proxyGroupRepoStub) Update(_ context.Context, proxy *Proxy) error {
	r.proxy = proxy
	return nil
}

func TestAdminProxyGroupUpdateMembership(t *testing.T) {
	group := int64(1)
	for _, test := range []struct {
		input    UpdateProxyInput
		expected *int64
	}{
		{input: UpdateProxyInput{Name: "rename"}, expected: &group},
		{input: UpdateProxyInput{ClearGroupID: true}, expected: nil},
		{input: UpdateProxyInput{GroupID: &group}, expected: &group},
	} {
		r := &proxyGroupRepoStub{proxy: &Proxy{ID: 2, GroupID: &group}}
		s := &adminServiceImpl{proxyRepo: r}
		proxy, err := s.UpdateProxy(context.Background(), 2, &test.input)
		require.NoError(t, err)
		require.Equal(t, test.expected, proxy.GroupID)
		require.Equal(t, test.input.GroupID != nil || test.input.ClearGroupID, proxy.GroupIDSet)
	}
}

func (r *proxyGroupRepoStub) ListProxyGroups(context.Context) ([]ProxyGroup, error) {
	return r.groups, nil
}
func (r *proxyGroupRepoStub) CreateProxyGroup(_ context.Context, name string) (*ProxyGroup, error) {
	return &ProxyGroup{ID: 1, Name: name}, nil
}
func (r *proxyGroupRepoStub) UpdateProxyGroup(_ context.Context, id int64, name string) (*ProxyGroup, error) {
	return &ProxyGroup{ID: id, Name: name}, nil
}
func (r *proxyGroupRepoStub) DeleteProxyGroup(context.Context, int64) error { return nil }
func (r *proxyGroupRepoStub) ProxyGroupExists(_ context.Context, id int64) (bool, error) {
	return id == 1, nil
}
func (r *proxyGroupRepoStub) AssignProxyGroup(_ context.Context, ids []int64, groupID *int64) (int64, error) {
	r.assigned, r.groupID = ids, groupID
	return int64(len(ids)), nil
}

func TestAdminProxyGroupValidation(t *testing.T) {
	s := &adminServiceImpl{proxyRepo: &proxyGroupRepoStub{}}
	group, err := s.CreateProxyGroup(context.Background(), "  Asia  ")
	require.NoError(t, err)
	require.Equal(t, "Asia", group.Name)
	_, err = s.CreateProxyGroup(context.Background(), "  ")
	require.Error(t, err)
	_, err = s.UpdateProxyGroup(context.Background(), 0, "Asia")
	require.Error(t, err)
}

func TestAdminAssignProxyGroupValidation(t *testing.T) {
	r := &proxyGroupRepoStub{}
	s := &adminServiceImpl{proxyRepo: r}
	groupID := int64(1)
	count, err := s.AssignProxyGroup(context.Background(), []int64{3, 3, 4}, &groupID)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	require.Equal(t, []int64{3, 4}, r.assigned)
	_, err = s.AssignProxyGroup(context.Background(), []int64{0}, nil)
	require.Error(t, err)
	_, err = s.AssignProxyGroup(context.Background(), nil, nil)
	require.Error(t, err)
	groupID = 9
	_, err = s.AssignProxyGroup(context.Background(), []int64{3}, &groupID)
	require.ErrorIs(t, err, ErrProxyGroupNotFound)
}
