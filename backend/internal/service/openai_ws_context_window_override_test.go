package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIWSContextWindowClientIdentity(t *testing.T) {
	payload := []byte(`{"previous_response_id":"resp_1","client_metadata":{"x-codex-window-id":"injected-window"}}`)
	for _, tc := range []struct {
		name    string
		window  string
		changed bool
	}{
		{name: "client unchanged", window: "client-window"},
		{name: "client omitted window"},
		{name: "client moved window", window: "next-window", changed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updated, boundary, err := normalizeOpenAIWSContextWindowBoundary(payload, "client-window", tc.window)
			require.NoError(t, err)
			require.Equal(t, tc.window, boundary.WindowID)
			require.Equal(t, tc.changed, boundary.Changed)
			require.Equal(t, tc.changed, boundary.PreviousResponseIDRemoved)
			require.Equal(t, !tc.changed, gjson.GetBytes(updated, "previous_response_id").Exists())
		})
	}
}
