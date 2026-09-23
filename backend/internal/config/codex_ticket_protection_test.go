package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketProtectionEmptyLengthsRemainJSONArray(t *testing.T) {
	for _, lengths := range [][]int{nil, {}} {
		protection := DefaultCodexTicketProtection()
		protection.RejectAndSilenceLengths = lengths
		got := (OpenAICodexTicketConfig{Protection: &protection}).TicketProtection()
		require.Equal(t, []int{}, got.RejectAndSilenceLengths)
		encoded, err := json.Marshal(got)
		require.NoError(t, err)
		require.Contains(t, string(encoded), `"reject_and_silence_lengths":[]`)
	}
}

func TestCodexTicketProtectionOmittedDefaultsAndSnapshotIsolation(t *testing.T) {
	got := (OpenAICodexTicketConfig{}).TicketProtection()
	require.Equal(t, []int{312}, got.RejectAndSilenceLengths)
	protection := DefaultCodexTicketProtection()
	got = (OpenAICodexTicketConfig{Protection: &protection}).TicketProtection()
	got.RejectAndSilenceLengths[0] = 356
	require.Equal(t, []int{312}, protection.RejectAndSilenceLengths)
}
