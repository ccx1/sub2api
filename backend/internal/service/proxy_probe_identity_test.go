package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func proxyProbeIdentityFixture() *Proxy {
	return &Proxy{ID: 7, Name: "original", Protocol: "http", Host: "proxy.example", Port: 8080,
		Username: "probe-user", Password: "probe-password", Status: StatusActive, UpdatedAt: time.Now()}
}

func TestProxyProbeIdentityKeepsMetadataEditsAndRejectsEgressEdits(t *testing.T) {
	proxy := proxyProbeIdentityFixture()
	info := &ProxyLatencyInfo{ProxyIdentity: ProxyProbeIdentity(proxy), UpdatedAt: proxy.UpdatedAt}
	cases := []struct {
		name string
		edit func(*Proxy)
		want bool
	}{
		{"name", func(p *Proxy) { p.Name = "renamed" }, true},
		{"group", func(p *Proxy) { p.GroupID = new(int64(9)) }, true},
		{"expiry", func(p *Proxy) { p.ExpiresAt = new(time.Now().Add(time.Hour)) }, true},
		{"fallback", func(p *Proxy) { p.FallbackMode = FallbackModeDirect }, true},
		{"protocol", func(p *Proxy) { p.Protocol = "socks5" }, false},
		{"host", func(p *Proxy) { p.Host = "new.example" }, false},
		{"port", func(p *Proxy) { p.Port++ }, false},
		{"username", func(p *Proxy) { p.Username = "new-user" }, false},
		{"password", func(p *Proxy) { p.Password = "new-password" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := *proxy
			tc.edit(&changed)
			changed.UpdatedAt = proxy.UpdatedAt.Add(time.Minute)
			require.Equal(t, tc.want, ProxyLatencyMatchesProxy(info, &changed))
		})
	}
}

func TestProxyProbeIdentityRequiresEvidenceAndKeepsLegacyTimestampGate(t *testing.T) {
	proxy := proxyProbeIdentityFixture()
	info := &ProxyLatencyInfo{UpdatedAt: proxy.UpdatedAt}
	require.True(t, ProxyLatencyMatchesProxy(info, proxy))
	proxy.UpdatedAt = proxy.UpdatedAt.Add(time.Second)
	require.False(t, ProxyLatencyMatchesProxy(info, proxy))
	require.False(t, ProxyLatencyMatchesProxy(nil, proxy))
	require.False(t, ProxyLatencyMatchesProxy(info, nil))
	info.ProxyIdentity, info.UpdatedAt = ProxyProbeIdentity(proxy), time.Time{}
	require.False(t, ProxyLatencyMatchesProxy(info, proxy))
	info.UpdatedAt = time.Now()
	info.ProxyIdentity = "unknown-identity"
	require.False(t, ProxyLatencyMatchesProxy(info, proxy))
}

type probeIdentityRepo struct {
	ProxyRepository
	proxy *Proxy
}

func (r *probeIdentityRepo) GetByID(context.Context, int64) (*Proxy, error) {
	return r.proxy, nil
}

type probeIdentityCache struct{ info *ProxyLatencyInfo }

func (c *probeIdentityCache) GetProxyLatencies(context.Context, []int64) (map[int64]*ProxyLatencyInfo, error) {
	return map[int64]*ProxyLatencyInfo{7: c.info}, nil
}

func (c *probeIdentityCache) SetProxyLatency(_ context.Context, _ int64, info *ProxyLatencyInfo) error {
	copy := *info
	c.info = &copy
	return nil
}

type probeIdentityProber func(context.Context, string) (*ProxyExitInfo, int64, error)

func (p probeIdentityProber) ProbeProxy(ctx context.Context, url string) (*ProxyExitInfo, int64, error) {
	return p(ctx, url)
}

func TestProxyProbeWritersBindLateResultsToRequestedEgress(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "automatic"}[automatic], func(t *testing.T) {
			proxy := proxyProbeIdentityFixture()
			original := *proxy
			cache := &probeIdentityCache{}
			svc := &adminServiceImpl{proxyRepo: &probeIdentityRepo{proxy: proxy}, proxyLatencyCache: cache}
			svc.proxyProber = probeIdentityProber(func(_ context.Context, url string) (*ProxyExitInfo, int64, error) {
				require.Equal(t, original.URL(), url)
				proxy.Host, proxy.UpdatedAt = "changed-during-probe.example", time.Now()
				return &ProxyExitInfo{CountryCode: "PH"}, 20, nil
			})
			if automatic {
				svc.probeProxyLatency(context.Background(), proxy)
			} else {
				result, err := svc.TestProxy(context.Background(), proxy.ID)
				require.NoError(t, err)
				require.True(t, result.Success)
			}
			require.NotNil(t, cache.info)
			require.Equal(t, "PH", cache.info.CountryCode)
			require.True(t, ProxyLatencyMatchesProxy(cache.info, &original))
			require.False(t, ProxyLatencyMatchesProxy(cache.info, proxy), "迟到结果不能认证新出口")
		})
	}
}

func TestProxyQualityFailureDoesNotReuseOldExitCountry(t *testing.T) {
	proxy := proxyProbeIdentityFixture()
	old := *proxy
	old.Host = "old.example"
	cache := &probeIdentityCache{info: &ProxyLatencyInfo{Success: true, CountryCode: "PH",
		ProxyIdentity: ProxyProbeIdentity(&old), UpdatedAt: time.Now()}}
	svc := &adminServiceImpl{proxyRepo: &probeIdentityRepo{proxy: proxy}, proxyLatencyCache: cache}
	svc.proxyProber = probeIdentityProber(func(context.Context, string) (*ProxyExitInfo, int64, error) {
		proxy.Host = "changed-during-probe.example"
		return nil, 0, errors.New("probe failed")
	})
	original := *proxy
	_, err := svc.CheckProxyQuality(context.Background(), proxy.ID)
	require.NoError(t, err)
	require.False(t, cache.info.Success)
	require.Empty(t, cache.info.CountryCode)
	require.True(t, ProxyLatencyMatchesProxy(cache.info, &original))
	require.False(t, ProxyLatencyMatchesProxy(cache.info, proxy))
}

func TestProxyLatencyQualityMergeDoesNotCrossEgressIdentity(t *testing.T) {
	for _, sameEgress := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed", true: "same"}[sameEgress], func(t *testing.T) {
			proxy := proxyProbeIdentityFixture()
			oldIdentity := ProxyProbeIdentity(proxy)
			if !sameEgress {
				proxy.Host = "new.example"
			}
			cache := &probeIdentityCache{info: &ProxyLatencyInfo{ProxyIdentity: oldIdentity, QualityStatus: "failed"}}
			svc := &adminServiceImpl{proxyLatencyCache: cache}
			svc.saveProxyLatency(context.Background(), proxy.ID, &ProxyLatencyInfo{
				Success: true, CountryCode: "PH", ProxyIdentity: ProxyProbeIdentity(proxy), UpdatedAt: time.Now(),
			})
			require.Equal(t, sameEgress, cache.info.QualityStatus == "failed")
		})
	}
}

func TestProxyLatencyDisplayRejectsOldEgressButKeepsMetadataEdits(t *testing.T) {
	for _, endpointChanged := range []bool{false, true} {
		t.Run(map[bool]string{false: "metadata", true: "egress"}[endpointChanged], func(t *testing.T) {
			proxy := proxyProbeIdentityFixture()
			cache := &probeIdentityCache{info: &ProxyLatencyInfo{Success: true, CountryCode: "PH",
				ProxyIdentity: ProxyProbeIdentity(proxy), UpdatedAt: proxy.UpdatedAt}}
			proxy.Name, proxy.UpdatedAt = "renamed", proxy.UpdatedAt.Add(time.Minute)
			if endpointChanged {
				proxy.Host = "new.example"
			}
			svc := &adminServiceImpl{proxyLatencyCache: cache}
			rows := []ProxyWithAccountCount{{Proxy: *proxy}}
			svc.attachProxyLatency(context.Background(), rows)
			if endpointChanged {
				require.Empty(t, rows[0].CountryCode)
				require.Empty(t, rows[0].LatencyStatus)
			} else {
				require.Equal(t, "PH", rows[0].CountryCode)
				require.Equal(t, "success", rows[0].LatencyStatus)
			}
		})
	}
}
