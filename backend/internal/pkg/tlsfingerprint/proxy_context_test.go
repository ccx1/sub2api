package tlsfingerprint

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPSProxyEncryptsConnectRequest(t *testing.T) {
	connected := make(chan bool, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connected <- r.TLS != nil && r.Method == http.MethodConnect && r.Host == "upstream.test:443"
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	dialer := NewHTTPProxyDialer(BuiltinProfile("nodejs24"), parsed)
	dialer.proxyTLSConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = dialer.DialTLSContext(ctx, "tcp", "upstream.test:443")
	require.ErrorContains(t, err, "502")
	select {
	case encrypted := <-connected:
		require.True(t, encrypted)
	default:
		t.Fatal("proxy did not receive encrypted CONNECT")
	}
}

func TestProxyTunnelHonorsRequestCancellation(t *testing.T) {
	for _, scheme := range []string{"http", "socks5"} {
		t.Run(scheme, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			finished := make(chan struct{})
			defer close(finished)
			go func() {
				conn, err := listener.Accept()
				if err == nil {
					defer conn.Close()
					<-finished
				}
			}()
			parsed, err := url.Parse(scheme + "://" + listener.Addr().String())
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			started := time.Now()
			if scheme == "http" {
				_, err = NewHTTPProxyDialer(nil, parsed).DialTLSContext(ctx, "tcp", "upstream.test:443")
			} else {
				_, err = NewSOCKS5ProxyDialer(nil, parsed).DialTLSContext(ctx, "tcp", "upstream.test:443")
			}
			require.Error(t, err)
			require.Less(t, time.Since(started), time.Second)
		})
	}
}
