package repository

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAstraModelBlockSurvivesSchedulerProjection(t *testing.T) {
	a := &service.Account{Extra: filterSchedulerExtra(map[string]any{"astra_model_disabled": true, "astra_model_blocked_keys": []any{"alias"}, "astra_model_empty_mapping": false, "secret": "discard"})}
	require.False(t, a.IsModelSupported("gpt-6-astra"))
	require.False(t, a.IsModelSupported("alias"))
	require.True(t, a.IsModelSupported("gpt-5"))
	require.NotContains(t, a.Extra, "secret")
}

func TestAstraRestoreOwnershipIgnoresBackgroundTimestampUpdates(t *testing.T) {
	state := astraActionState{Mode: "account"}
	require.False(t, astraRestoreAccountConflict(state, true, false, nil, nil))
	require.True(t, astraRestoreAccountConflict(state, true, true, nil, nil), "manual schedulable change remains a conflict")

	model := astraActionState{Mode: "model", AfterMapping: []byte(`{"other":"model"}`)}
	require.False(t, astraRestoreAccountConflict(model, true, false, []byte(`{"other":"model"}`), map[string]any{"astra_model_disabled": true}))
	require.True(t, astraRestoreAccountConflict(model, true, false, []byte(`{"manual":"model"}`), map[string]any{"astra_model_disabled": true}))
}
