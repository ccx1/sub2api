package service

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

type randomProxyFailureStub struct {
	accounts, proxies []int64
	err               error
}

func (s *randomProxyFailureStub) ReportRandomProxyFailure(_ context.Context, accountID, proxyID int64) error {
	s.accounts = append(s.accounts, accountID)
	s.proxies = append(s.proxies, proxyID)
	return s.err
}

func TestRandomProxyTransportFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		reported bool
	}{
		{"refused", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
		{"proxy authentication", errors.New("proxy authentication required"), true},
		{"reset", errors.New("connection reset by peer"), true},
		{"closed", io.ErrUnexpectedEOF, true},
		{"canceled", context.Canceled, false},
		{"request deadline", context.DeadlineExceeded, false},
		{"business auth", errors.New("upstream status 401"), false},
		{"rate limit", errors.New("upstream status 429"), false},
		{"invalid policy", errors.New("invalid TLS profile"), false},
		{"success", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := randomProxyAccount("")
			a.ID = 42
			id := int64(7)
			a.ProxyID = &id
			reporter := &randomProxyFailureStub{}
			require.Equal(t, tc.reported, ReportRandomProxyTransportFailure(context.Background(), a, reporter, tc.err))
			if tc.reported {
				require.Equal(t, []int64{42}, reporter.accounts)
				require.Equal(t, []int64{7}, reporter.proxies)
			} else {
				require.Empty(t, reporter.accounts)
			}
		})
	}
}

func TestRandomProxyFailureDoesNotEvictOnClientCancelOrFixedProxy(t *testing.T) {
	a := randomProxyAccount("")
	a.ID = 42
	id := int64(7)
	a.ProxyID = &id
	reporter := &randomProxyFailureStub{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, ReportRandomProxyTransportFailure(ctx, a, reporter, io.ErrUnexpectedEOF))
	a.Extra = nil
	require.False(t, ReportRandomProxyTransportFailure(context.Background(), a, reporter, io.ErrUnexpectedEOF))
	require.Empty(t, reporter.accounts)
}
