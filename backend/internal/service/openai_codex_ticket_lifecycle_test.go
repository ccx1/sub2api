package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type codexTicketFuncUpstream struct {
	HTTPUpstream
	do func(*http.Request) (*http.Response, error)
}

func (u *codexTicketFuncUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.do(req)
}
func codexTicketResponse() *http.Response {
	return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292))
}

func TestCodexTicketProbeBypassesPluginDuringWiring(t *testing.T) {
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{pluginID: 1, rolloutPercent: 100, unavailable: "plugin must not handle synthetic probes"})
	var calls atomic.Int64
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if HTTPUpstreamProfileFromContext(req.Context()) != HTTPUpstreamProfileOpenAIHarvest || !req.Close {
			return nil, errors.New("missing no-reuse transport profile")
		}
		return codexTicketResponse(), nil
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, upstream)
	svc.SetPluginManager(manager)
	account := ticketTestAccount(41)
	// This binding rejects ordinary traffic; harvesting still uses the dedicated transport.
	request, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	_, err := svc.doOpenAIUpstream(request, "", account)
	require.Error(t, err)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 200; i++ {
			svc.SetPluginManager(manager)
		}
	}()
	close(start)
	for i := 0; i < 20; i++ {
		state, status, err := svc.fireOpenAICodexTicketProbe(context.Background(), account, "test-token", "gpt-6-astra", "http://proxy.example.com:8080", time.Second)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
		require.Len(t, state, 292)
	}
	wg.Wait()
	require.Equal(t, int64(20), calls.Load())
}

type codexTicketLifecycleRepo struct {
	AccountRepository
	account Account
	list    func(context.Context) ([]Account, error)
	persist func(context.Context) error
}

func (r *codexTicketLifecycleRepo) ListByPlatform(ctx context.Context, _ string) ([]Account, error) {
	if r.list != nil {
		return r.list(ctx)
	}
	return []Account{r.account}, nil
}
func (r *codexTicketLifecycleRepo) UpdateExtra(ctx context.Context, _ int64, _ map[string]any) error {
	if r.persist != nil {
		return r.persist(ctx)
	}
	return nil
}

type codexTicketLifecycleSettings struct {
	SettingRepository
	get func(context.Context, string) (string, error)
}

func (r *codexTicketLifecycleSettings) GetValue(ctx context.Context, key string) (string, error) {
	return r.get(ctx, key)
}

func (r *codexTicketLifecycleSettings) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string)
	for _, key := range keys {
		value, err := r.get(ctx, key)
		if errors.Is(err, ErrSettingNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

func TestCodexTicketHarvesterStopCancelsInFlightWork(t *testing.T) {
	for _, stage := range []string{"settings-enabled", "settings-policy", "settings-proxy", "accounts", "upstream", "persist"} {
		t.Run(stage, func(t *testing.T) {
			started := make(chan struct{})
			cancelled := make(chan struct{})
			var once sync.Once
			block := func(ctx context.Context) error {
				once.Do(func() { close(started) })
				<-ctx.Done()
				close(cancelled)
				return ctx.Err()
			}
			account := ticketTestAccount(41)
			account.Status = StatusActive
			repo := &codexTicketLifecycleRepo{account: *account}
			upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				if stage == "upstream" {
					return nil, block(req.Context())
				}
				return codexTicketResponse(), nil
			}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://proxy.example.com:8080", HarvestAttemptTimeoutSeconds: 25, Models: []string{"gpt-6-astra"}}, upstream)
			svc.accountRepo = repo
			if stage == "accounts" {
				repo.list = func(ctx context.Context) ([]Account, error) { return nil, block(ctx) }
			}
			if stage == "persist" {
				repo.persist = block
			}
			if strings.HasPrefix(stage, "settings-") {
				svc.settingService = NewSettingService(&codexTicketLifecycleSettings{get: func(ctx context.Context, key string) (string, error) {
					if stage == "settings-enabled" && key == SettingKeyOpenAICodexTicketEnabled || stage == "settings-policy" && key == SettingKeyCodexTicketPolicy || stage == "settings-proxy" && key == SettingKeyOpenAICodexTicketHarvestProxyURL {
						return "", block(ctx)
					}
					if key == SettingKeyOpenAICodexTicketEnabled {
						return "true", nil
					}
					return "", ErrSettingNotFound
				}}, svc.cfg)
			}
			svc.StartOpenAICodexTicketHarvester()
			t.Cleanup(svc.StopOpenAICodexTicketHarvester)
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("harvester did not reach " + stage)
			}
			// Repeated start must not create a second loop or overwrite the cancellation state.
			svc.StartOpenAICodexTicketHarvester()
			stopped := make(chan struct{})
			go func() { svc.StopOpenAICodexTicketHarvester(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("stop waited for the probe timeout")
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("in-flight operation did not receive cancellation")
			}
			svc.StopOpenAICodexTicketHarvester()
			svc.StartOpenAICodexTicketHarvester()
		})
	}
}

type codexTicketHeaderOnlyBody struct{ reads, closes int }

func (b *codexTicketHeaderOnlyBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *codexTicketHeaderOnlyBody) Close() error             { b.closes++; return nil }
func TestCodexTicketProbeReadsAndRejectsHeaderOnlyStream(t *testing.T) {
	body := &codexTicketHeaderOnlyBody{}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		response := codexTicketResponse()
		response.Body = body
		return response, nil
	}})
	_, _, err := svc.fireOpenAICodexTicketProbe(context.Background(), ticketTestAccount(41), "test-token", "gpt-6-astra", "", time.Second)
	require.Error(t, err)
	require.Positive(t, body.reads)
	require.Equal(t, 1, body.closes)
}

type codexTicketProbeBody struct {
	io.Reader
	closed bool
}

func (b *codexTicketProbeBody) Close() error { b.closed = true; return nil }

func TestCodexTicketProbeIOValidationAndClosure(t *testing.T) {
	for _, status := range []int{200, 401, 403, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			response := codexTicketCompletedResponse("gpt-6-astra", "")
			body := &codexTicketProbeBody{Reader: response.Body}
			response.Body, response.StatusCode = body, status
			upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "Bearer secret-token", req.Header.Get("Authorization"))
				require.Equal(t, "acc-1", req.Header.Get("Chatgpt-Account-Id"))
				require.Equal(t, fakeCodexTicketState(292), req.Header.Get(openAICodexTurnStateHeader))
				require.Equal(t, "identity", req.Header.Get("Accept-Encoding"))
				require.True(t, req.Close)
				return response, nil
			}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, upstream)
			state, actual, err := svc.probeOpenAICodexTicket(context.Background(), openAICodexTicketProbeInput{Account: ticketTestAccount(41), Token: "secret-token", Model: "gpt-6-astra", State: fakeCodexTicketState(292), Timeout: time.Second})
			require.Equal(t, status, actual)
			require.Empty(t, state)
			require.Equal(t, status != 200, err != nil)
			var transportError *codexTicketTransportError
			require.False(t, errors.As(err, &transportError))
			require.True(t, body.closed)
		})
	}
}

type codexTicketProbeReadError struct{ error }

func (e codexTicketProbeReadError) Read([]byte) (int, error) { return 0, e.error }

func TestCodexTicketProbeReadFailureAndLimit(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		t.Run(map[bool]string{false: "read-error", true: "oversize"}[oversized], func(t *testing.T) {
			var reader io.Reader = codexTicketProbeReadError{errors.New("http://user:secret@proxy.example transport error")}
			if oversized {
				reader = strings.NewReader(strings.Repeat("x", (4<<20)+1))
			}
			body := &codexTicketProbeBody{Reader: reader}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: body}, nil
			}})
			_, status, err := svc.fireOpenAICodexTicketProbe(context.Background(), ticketTestAccount(41), "secret-token", "gpt-6-astra", "", time.Second)
			require.Error(t, err)
			require.Equal(t, 200, status)
			var transportError *codexTicketTransportError
			require.Equal(t, !oversized, errors.As(err, &transportError))
			require.NotContains(t, err.Error(), "secret")
			require.NotContains(t, err.Error(), "proxy.example")
			require.True(t, body.closed)
		})
	}
}

type codexTicketProbeTLSUpstream struct {
	HTTPUpstream
	t     *testing.T
	calls int
}

func (u *codexTicketProbeTLSUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.calls++
	require.NotNil(u.t, profile)
	require.Equal(u.t, int64(41), accountID)
	require.Equal(u.t, "http://proxy.example:8080", proxy)
	require.Equal(u.t, HTTPUpstreamProfileOpenAIHarvest, HTTPUpstreamProfileFromContext(req.Context()))
	require.True(u.t, req.Close)
	return codexTicketResponse(), nil
}

func TestCodexTicketProbeKeepsConfiguredTLSProfile(t *testing.T) {
	u := &codexTicketProbeTLSUpstream{t: t}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, u)
	svc.cfg.Gateway.TLSFingerprint.Enabled = true
	account := ticketTestAccount(41)
	account.Extra = map[string]any{AntiDegradationExtraKey: true, "tls_fingerprint_builtin": "nodejs24"}
	_, status, err := svc.fireOpenAICodexTicketProbe(context.Background(), account, "secret-token", "gpt-6-astra", "http://proxy.example:8080", time.Second)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, 1, u.calls)
}

func TestCodexTicketPolicyExemptsCredentialShadows(t *testing.T) {
	parentID := int64(41)
	parent := ticketTestAccount(parentID)
	shadow := ticketTestAccount(42)
	shadow.ParentAccountID = &parentID
	shadow.Status = StatusActive
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, HarvestProxyURL: "http://proxy.example.com:8080"}
	upstream := &httpUpstreamRecorder{}
	svc := ticketTestService(t, cfg, upstream)
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*shadow}}
	require.True(t, svc.openAICodexTicketBlocksAccount(parent, "gpt-6-astra"))
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		shadow.Type = accountType
		require.False(t, svc.openAICodexTicketBlocksAccount(shadow, "gpt-6-astra"))
		headers := http.Header{}
		headers.Set(openAICodexTurnStateHeader, "client-state")
		require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), shadow, "gpt-6-astra", headers))
		require.Equal(t, "client-state", headers.Get(openAICodexTurnStateHeader))
		require.Empty(t, OpenAICodexTicketStatuses(shadow, cfg, time.Now()))
		svc.probeOnceOpenAICodexTicket(context.Background(), shadow, "gpt-6-astra")
	}
	svc.refreshOpenAICodexTickets(context.Background())
	require.Empty(t, upstream.requests)
}
