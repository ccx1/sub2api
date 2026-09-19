package service

import (
	"context"
	"crypto/tls"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestProtectionWSUsesConfiguredClientHello(t *testing.T) {
	hellos := make(chan *tls.ClientHelloInfo, 1)
	server := httptest.NewUnstartedServer(nil)
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		hellos <- hello
		return nil, nil
	}}
	server.StartTLS()
	defer server.Close()
	dialer := newDefaultOpenAIWSClientDialer().(openAIWSClientTLSDialer)
	profile := tlsfingerprint.BuiltinProfile("nodejs22")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, _, err := dialer.DialWithTLS(ctx, strings.Replace(server.URL, "https://", "wss://", 1), nil, "", profile)
	require.Error(t, err, "本地证书未加入信任库，仍必须验证服务端证书")
	select {
	case hello := <-hellos:
		require.Equal(t, profile.CipherSuites, hello.CipherSuites)
		require.Equal(t, []string{"http/1.1"}, hello.SupportedProtos)
	default:
		t.Fatal("未观察到实际 TLS ClientHello")
	}
}

func TestProtectionWSTransportSeparatesProxyAndFullProfile(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer().(*coderOpenAIWSClientDialer)
	profile := tlsfingerprint.BuiltinProfile("nodejs24")
	first, err := dialer.fingerprintHTTPClient("wss://upstream.test/responses", "http://localhost:8080", profile)
	require.NoError(t, err)
	same, err := dialer.fingerprintHTTPClient("wss://upstream.test/responses", "http://localhost:8080", profile)
	require.NoError(t, err)
	require.Same(t, first, same)
	otherProxy, err := dialer.fingerprintHTTPClient("wss://upstream.test/responses", "http://localhost:8081", profile)
	require.NoError(t, err)
	require.NotSame(t, first, otherProxy)
	changed := profile.Clone()
	changed.CipherSuites[0], changed.CipherSuites[1] = changed.CipherSuites[1], changed.CipherSuites[0]
	otherProfile, err := dialer.fingerprintHTTPClient("wss://upstream.test/responses", "http://localhost:8080", changed)
	require.NoError(t, err)
	require.NotSame(t, first, otherProfile)
}
