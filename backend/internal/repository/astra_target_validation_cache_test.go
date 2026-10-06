package repository

import (
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestAstraValidatedTargetBypassesBusyProbe(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.CodexGatewayPin = pinConfig()
	probes := 0
	wrapper := &astraRoutingUpstream{cfg: cfg}
	wrapper.delegate = gatewayPinDelegate{call: func(_ *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		probes++
		return pinResponse(""), nil
	}}
	pool := wrapper.current(t.Context())
	now := time.Now()
	pool.recordSource(299, now, codexGatewayRouteFromResponse(pinResponse("__oailb=route; Path=/; Secure; Max-Age=230"), "/backend-api/codex/responses", now), true)
	_, _, _, release, err := wrapper.targetRouteOnce(pinRequest(t), "", 300, 1, nil)
	release()
	require.NoError(t, err)
	require.Equal(t, 2, probes)

	pool.targetProbeMu.Lock()
	defer pool.targetProbeMu.Unlock()
	cookie, _, _, release, err := wrapper.targetRouteOnce(pinRequest(t), "", 300, 1, nil)
	release()
	require.NoError(t, err)
	require.Equal(t, "route", cookie.Value)
	require.Equal(t, 2, probes)
	_, _, _, release, err = wrapper.targetRouteOnce(pinRequest(t), "changed-egress", 300, 1, nil)
	release()
	require.EqualError(t, err, "target_validation_in_progress", "different route identity must not reuse cached evidence")
	err = wrapper.VerifyAstraGatewayTarget(t.Context(), pinRequest(t), "", 300, 1)
	require.EqualError(t, err, "target_validation_in_progress", "manual verification must remain forced")
}
