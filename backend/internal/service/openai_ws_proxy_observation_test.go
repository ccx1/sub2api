package service

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type observedProxyWSFrameStub struct {
	openaiwsv2.FrameConn
	readErr error
	payload []byte
	writes  int
	closes  int
}

func (c *observedProxyWSFrameStub) ReadFrame(context.Context) (coderws.MessageType, []byte, error) {
	return coderws.MessageText, c.payload, c.readErr
}

func (c *observedProxyWSFrameStub) WriteFrame(context.Context, coderws.MessageType, []byte) error {
	c.writes++
	return nil
}

func (c *observedProxyWSFrameStub) Close() error {
	c.closes++
	return nil
}

type observedProxyWSRepo struct {
	balancedAccountProxyStub
	randomProxyFailureStub
}

func TestRandomProxyObservedWSReadFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		canceled bool
		reported bool
	}{
		{"reset", &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}, false, true},
		{"unexpected eof", io.ErrUnexpectedEOF, false, true},
		{"abnormal close", coderws.CloseError{Code: coderws.StatusAbnormalClosure}, false, true},
		{"canceled read", context.Canceled, false, false},
		{"client gone", io.ErrUnexpectedEOF, true, false},
		{"normal close", coderws.CloseError{Code: coderws.StatusNormalClosure, Reason: "connection reset"}, false, false},
		{"policy close", coderws.CloseError{Code: coderws.StatusPolicyViolation, Reason: "proxyconnect tcp"}, false, false},
		{"upstream 401", errors.New("upstream status 401"), false, false},
		{"upstream 429", errors.New("upstream status 429"), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyReject)
			proxyID := int64(7)
			account.ProxyID = &proxyID
			reporter := &randomProxyFailureStub{}
			inner := &observedProxyWSFrameStub{readErr: tc.err}
			conn := &randomProxyObservedWSFrameConn{FrameConn: inner, account: account, source: reporter}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			_, _, err := conn.ReadFrame(ctx)
			require.ErrorIs(t, err, tc.err)
			if tc.reported {
				require.Equal(t, []int64{7}, reporter.proxies)
			} else {
				require.Empty(t, reporter.proxies)
			}
			require.Zero(t, inner.writes, "读取失败不能隐式重放请求")
		})
	}
}

func TestRandomProxyObservedWSRoutingChangeClosesBeforeWrite(t *testing.T) {
	current := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyReject)
	original := &Proxy{ID: 7, Status: StatusActive, Protocol: "http", Host: "old.example", Port: 8080}
	selected := &Proxy{ID: 8, Status: StatusActive, Protocol: "http", Host: "new.example", Port: 8080}
	bound := *current
	bound.ProxyID, bound.Proxy = &original.ID, original
	repo := &observedProxyWSRepo{balancedAccountProxyStub: balancedAccountProxyStub{
		pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{account: current, proxy: selected},
	}}
	inner := &observedProxyWSFrameStub{readErr: io.ErrUnexpectedEOF}
	conn := &randomProxyObservedWSFrameConn{FrameConn: inner, account: &bound, source: repo}
	err := conn.WriteFrame(context.Background(), coderws.MessageText, []byte(`{"type":"response.create"}`))
	require.ErrorIs(t, err, ErrRandomProxyChanged)
	require.Zero(t, inner.writes)
	require.Equal(t, 1, inner.closes)
	_, _, readErr := conn.ReadFrame(context.Background())
	require.ErrorIs(t, readErr, io.ErrUnexpectedEOF)
	require.Empty(t, repo.proxies, "主动关闭旧连接不能把健康出口标成网络故障")
}

func TestRandomProxyObservedWSKeepsBusinessEvents(t *testing.T) {
	for _, code := range []string{"401", "429"} {
		inner := &observedProxyWSFrameStub{payload: []byte(`{"type":"error","status":` + code + `}`)}
		reporter := &randomProxyFailureStub{}
		account := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyReject)
		proxyID := int64(7)
		account.ProxyID = &proxyID
		conn := &randomProxyObservedWSFrameConn{FrameConn: inner, account: account, source: reporter}
		_, payload, err := conn.ReadFrame(context.Background())
		require.NoError(t, err)
		require.Equal(t, inner.payload, payload)
		require.Empty(t, reporter.proxies)
	}
}
