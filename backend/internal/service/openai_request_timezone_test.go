package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRewriteOpenAIRequestTimezone(t *testing.T) {
	body := []byte(`{"input":[{"content":[{"text":"<environment_context>\n<timezone>Asia/Shanghai</timezone>\n</environment_context>"}]}],"tools":[{"function":{"arguments":"{\"timezone\":\"Asia/Shanghai\"}"}}],"opaque":9007199254740993}`)

	out, changed, err := RewriteOpenAIRequestTimezone(body, "America/New_York")
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, gjson.GetBytes(out, "input.0.content.0.text").String(), "<timezone>America/New_York</timezone>")
	require.Contains(t, string(out), `"opaque":9007199254740993`)
	require.Equal(t, `{"timezone":"Asia/Shanghai"}`, gjson.GetBytes(out, "tools.0.function.arguments").String())

	rewrittenAgain, changedAgain, err := RewriteOpenAIRequestTimezone(out, "America/New_York")
	require.NoError(t, err)
	require.False(t, changedAgain)
	require.Equal(t, out, rewrittenAgain)
}

func TestRewriteOpenAIRequestTimezoneOnlyTouchesSupportedMetadata(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{name: "ordinary text", body: []byte(`{"input":"<timezone>Asia/Shanghai</timezone>"}`)},
		{name: "tool environment text", body: []byte(`{"tools":[{"function":{"arguments":"<environment_context><timezone>Asia/Shanghai</timezone></environment_context>"}}]}`)},
		{name: "tool metadata", body: []byte(`{"tools":[{"function":{"arguments":{"metadata":{"timezone":"Asia/Shanghai"}}}}]}`)},
		{name: "tool argument location", body: []byte(`{"tools":[{"function":{"arguments":{"user_location":{"timezone":"Asia/Shanghai"}}}}]}`)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out, changed, err := RewriteOpenAIRequestTimezone(tt.body, "UTC")
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, tt.body, out)
		})
	}
	webSearch := []byte(`{"tools":[{"type":"web_search","user_location":{"timezone":"Asia/Shanghai"}}]}`)
	out, changed, err := RewriteOpenAIRequestTimezone(webSearch, "UTC")
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "UTC", gjson.GetBytes(out, "tools.0.user_location.timezone").String())

	for _, tt := range []struct {
		name string
		body string
		path string
	}{
		{name: "top-level metadata", body: `{"timezone":"Asia/Shanghai"}`, path: "timezone"},
		{name: "session metadata", body: `{"session":{"timezone":"Asia/Shanghai"}}`, path: "session.timezone"},
		{name: "metadata object", body: `{"metadata":{"timezone":"Asia/Shanghai"}}`, path: "metadata.timezone"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, changed, err := RewriteOpenAIRequestTimezone([]byte(tt.body), "Europe/London")
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, "Europe/London", gjson.GetBytes(out, tt.path).String())
		})
	}
}

func TestRewriteOpenAIRequestTimezoneNoopAndValidation(t *testing.T) {
	body := []byte(`{"user_location":{"timezone":"Asia/Shanghai"}}`)
	out, changed, err := RewriteOpenAIRequestTimezone(body, "")
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, out)
	trailing := []byte(`{"input":"<environment_context><timezone>Asia/Shanghai</timezone></environment_context>"} trailing`)
	out, changed, err = RewriteOpenAIRequestTimezone(trailing, "UTC")
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, trailing, out)

	_, _, err = RewriteOpenAIRequestTimezone(body, "Mars/Olympus")
	require.Error(t, err)
	_, _, err = RewriteOpenAIRequestTimezone(body, "Local")
	require.Error(t, err)
}
