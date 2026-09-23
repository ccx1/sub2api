package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedDefaultGroupsDecodeLegacyAndArrays(t *testing.T) {
	var groups SharedPoolDefaultGroupIDs
	require.NoError(t, json.Unmarshal([]byte(`{"openai":4,"gemini":[8,9],"anthropic":[]}`), &groups))
	require.Equal(t, SharedPoolDefaultGroupIDs{"openai": {4}, "gemini": {8, 9}, "anthropic": {}}, groups)
	raw, err := json.Marshal(groups)
	require.NoError(t, err)
	require.JSONEq(t, `{"openai":[4],"gemini":[8,9],"anthropic":[]}`, string(raw))
	for _, invalid := range []string{`[]`, `{"openai":"4"}`, `{"openai":1.5}`, `{"openai":[4,1.5]}`, `{"openai":{}}`} {
		previous, err := json.Marshal(groups)
		require.NoError(t, err)
		require.Error(t, json.Unmarshal([]byte(invalid), &groups), invalid)
		current, err := json.Marshal(groups)
		require.NoError(t, err)
		require.JSONEq(t, string(previous), string(current))
	}
}

func TestSharedDefaultGroupsNormalize(t *testing.T) {
	input := SharedPoolDefaultGroupIDs{PlatformOpenAI: {8, 4, 8}, PlatformGemini: {}}
	normalized, err := NormalizeSharedDefaultGroupIDs(input)
	require.NoError(t, err)
	require.Equal(t, SharedPoolDefaultGroupIDs{PlatformOpenAI: {8, 4}}, normalized)
	require.Equal(t, []int64{8, 4, 8}, input[PlatformOpenAI])
	tooMany := make([]int64, 51)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	for _, invalid := range []SharedPoolDefaultGroupIDs{
		{PlatformOpenAI: {0}}, {PlatformOpenAI: {-1}}, {"unsupported": {4}}, {PlatformOpenAI: tooMany},
	} {
		_, err := NormalizeSharedDefaultGroupIDs(invalid)
		require.Error(t, err)
	}
}
