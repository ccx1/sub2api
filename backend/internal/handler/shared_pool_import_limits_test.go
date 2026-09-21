//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedImportDefaultNameLimitAndNumberedSuffix(t *testing.T) {
	defaults := importTestDefaults()
	defaults.Name = strings.Repeat("名", 101)
	_, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: "one"}}, Defaults: defaults})
	require.Error(t, err)
	defaults.Name = strings.Repeat("名", 100)
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: "one\ntwo"}}, Defaults: defaults})
	require.NoError(t, err)
	for i, suffix := range []string{" #1", " #2"} {
		require.Len(t, []rune(entries[i].input.Name), 100)
		require.True(t, strings.HasSuffix(entries[i].input.Name, suffix))
	}
}

func TestSharedImportRejectsLongSourceNameBeforeWriting(t *testing.T) {
	content, err := json.Marshal(map[string]any{"name": strings.Repeat("名", 10000), "access_token": "token"})
	require.NoError(t, err)
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: string(content)}}, Defaults: importTestDefaults()})
	require.NoError(t, err)
	require.NotEmpty(t, entries[0].item.Message)
	require.LessOrEqual(t, len([]rune(entries[0].item.Name)), 100)
	result, err := executeSharedImport(context.Background(), 905, entries, func(context.Context, int64, service.SharedPoolAccountInput) (*service.SharedPoolAccountView, error) {
		t.Fatal("long account name should not be written")
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
}

func TestSharedImportResponseFitsIdempotencyLimitAfterJSONEscaping(t *testing.T) {
	values := make([]map[string]any, 50)
	for i := range values {
		values[i] = map[string]any{"name": strings.Repeat("<", 100), "access_token": "token"}
	}
	content, err := json.Marshal(values)
	require.NoError(t, err)
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Name: strings.Repeat("<", 200), Content: string(content)}}, Defaults: importTestDefaults()})
	require.NoError(t, err)
	result, err := executeSharedImport(context.Background(), 906, entries, func(context.Context, int64, service.SharedPoolAccountInput) (*service.SharedPoolAccountView, error) {
		return nil, infraerrors.BadRequest("UPSTREAM_REJECTED", strings.Repeat("<", 10000))
	})
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Less(t, len(encoded), 60<<10)
	require.Equal(t, 50, result.Failed)
	warnings, err := json.Marshal(result.Warnings)
	require.NoError(t, err)
	require.LessOrEqual(t, len(warnings), 4<<10)
}
