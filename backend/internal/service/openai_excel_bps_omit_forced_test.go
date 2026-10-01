package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type omitForcedChoiceCase struct {
	name   string
	choice map[string]any
	tools  []any
}

func TestExcelBPSOmitToolsForcedSelectionRejected(t *testing.T) {
	function := map[string]any{"type": "function", "name": "lookup_client", "parameters": map[string]any{"type": "object"}}
	custom := map[string]any{"type": "custom", "name": "run_client"}
	hosted := map[string]any{"type": "web_search", "external_web_access": true}
	catalog := []any{function, custom, hosted}
	cases := []omitForcedChoiceCase{
		{"function", map[string]any{"type": "function", "name": "lookup_client"}, catalog},
		{"custom", map[string]any{"type": "custom", "name": "run_client"}, catalog},
	}
	for _, mode := range []string{"auto", "required"} {
		for _, allOmitted := range []bool{false, true} {
			references := []any{map[string]any{"type": "web_search"}}
			tools := []any{hosted}
			if !allOmitted {
				references = append(references, map[string]any{"type": "function", "name": "lookup_client"})
				tools = catalog
			}
			cases = append(cases, omitForcedChoiceCase{
				name:   fmt.Sprintf("allowed_tools/mode=%s/all_omitted=%t", mode, allOmitted),
				choice: map[string]any{"type": "allowed_tools", "mode": mode, "tools": references},
				tools:  tools,
			})
		}
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				assertOmitToolsForcedRejected(t, tc, stream)
			})
		}
	}
}

func assertOmitToolsForcedRejected(t *testing.T, tc omitForcedChoiceCase, stream bool) {
	t.Helper()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(excelBPSToolPolicyResponse)),
	}}
	account := excelAccount()
	account.Extra[ExcelBPSOmitUnsupportedToolsKey] = true
	body, err := json.Marshal(map[string]any{
		"model": "gpt-6-astra", "input": "hello", "stream": stream,
		"tools": tc.tools, "tool_choice": tc.choice,
	})
	require.NoError(t, err)
	ctx := context.Background()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	_, err = openAIClientToolsTestService(upstream).Forward(ctx, c, account, body)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "tool_choice auto or none")
	require.Empty(t, upstream.requests, "a restrictive choice must never be weakened into an upstream request")
	require.True(t, IsResponseCommitted(c), "the handler must not append another error or retry")
}
