package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type scopedRandomProxyStub struct {
	randomProxySelectorStub
	ids []int64
}

func (s *scopedRandomProxyStub) SelectRandomActiveProxyFromPool(_ context.Context, ids []int64) (*Proxy, error) {
	s.ids = append([]int64(nil), ids...)
	return s.proxy, s.err
}

func TestResolveRandomProxySelectedPoolNeverUsesGlobalPool(t *testing.T) {
	a := randomProxyAccount(RandomProxyEmptyPoolPolicyReject)
	a.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolSelected
	a.Extra[RandomProxyPoolIDsExtraKey] = []any{float64(9), float64(7), float64(7)}
	selector := &scopedRandomProxyStub{randomProxySelectorStub: randomProxySelectorStub{proxy: &Proxy{ID: 7, Status: StatusActive}}}
	require.NoError(t, ResolveRandomProxy(context.Background(), a, selector))
	require.Equal(t, []int64{7, 9}, selector.ids)
	require.Zero(t, selector.calls)
	require.Equal(t, int64(7), *a.ProxyID)
	selector.proxy = &Proxy{ID: 30, Status: StatusActive}
	require.ErrorIs(t, ResolveRandomProxy(context.Background(), a, selector), ErrRandomProxyUnavailable)
	require.Nil(t, a.ProxyID)
}

func TestResolveRandomProxyEmptySelectedPoolPolicies(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable, RandomProxyEmptyPoolPolicyDirect} {
		t.Run(policy, func(t *testing.T) {
			a := randomProxyAccount(policy)
			a.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolSelected
			selector := &scopedRandomProxyStub{randomProxySelectorStub: randomProxySelectorStub{proxy: &Proxy{ID: 7, Status: StatusActive}}}
			err := ResolveRandomProxy(context.Background(), a, selector)
			if policy == RandomProxyEmptyPoolPolicyDirect {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrRandomProxyUnavailable)
				require.Equal(t, policy, RandomProxyUnavailablePolicy(err))
			}
			require.Nil(t, a.ProxyID)
			require.Zero(t, selector.calls)
			require.Nil(t, selector.ids)
		})
	}
}

func TestValidateRandomProxyPoolExtra(t *testing.T) {
	for _, raw := range []string{
		`{"random_proxy_pool_scope":"selected","random_proxy_pool_ids":[]}`,
		`{"random_proxy_pool_scope":" selected ","random_proxy_pool_ids":[]}`,
		`{"random_proxy_pool_scope":"unknown"}`,
		`{"random_proxy_pool_scope":"selected","random_proxy_pool_ids":[1.5]}`,
		`{"random_proxy_pool_scope":"selected","random_proxy_pool_ids":[-1]}`,
		`{"random_proxy_pool_scope":"selected","random_proxy_pool_ids":{}}`,
	} {
		var extra map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &extra))
		require.Error(t, ValidateRandomProxyPoolExtra(extra), raw)
	}
	require.NoError(t, ValidateRandomProxyPoolExtra(nil))
	require.NoError(t, ValidateRandomProxyPoolExtra(map[string]any{RandomProxyPoolScopeExtraKey: RandomProxyPoolAll, RandomProxyPoolIDsExtraKey: []int64{}}))
	require.NoError(t, ValidateRandomProxyPoolExtra(map[string]any{RandomProxyPoolScopeExtraKey: RandomProxyPoolSelected, RandomProxyPoolIDsExtraKey: []int64{7, 9}}))
}

type randomProxyUsageStub struct{ records []RandomProxyUsage }

func (s *randomProxyUsageStub) RecordRandomProxyUsage(_ context.Context, _ int64, v RandomProxyUsage) error {
	s.records = append(s.records, v)
	return nil
}

func TestRandomProxyUsageOnlyRecordsSafeOutboundMetadata(t *testing.T) {
	recorder := &randomProxyUsageStub{}
	a := randomProxyAccount(RandomProxyEmptyPoolPolicyDirect)
	RecordRandomProxyUsage(context.Background(), &Account{}, recorder)
	require.Empty(t, recorder.records)
	RecordRandomProxyUsage(context.Background(), a, recorder)
	require.Nil(t, recorder.records[0].ProxyID)
	id := int64(7)
	a.ProxyID, a.Proxy = &id, &Proxy{ID: id, Name: "region-a", Host: "proxy.example", Port: 8080, Username: "private-user", Password: "private-password"}
	RecordRandomProxyUsage(context.Background(), a, recorder)
	require.Equal(t, &id, recorder.records[1].ProxyID)
	encoded, err := json.Marshal(recorder.records[1])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-")
	require.Contains(t, string(encoded), "region-a")
}
