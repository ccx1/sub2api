package basispoints

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestContentValidationPreservesInlineImageCause(t *testing.T) {
	b := &Bridge{}
	err := b.validateHistoryContent([]any{object{"type": "input_image", "image_url": "data:image/png;base64,PRIVATE"}}, 4, "content")
	require.ErrorIs(t, err, ErrInlineImage)
	var contentErr *ContentValidationError
	require.ErrorAs(t, err, &contentErr)
	require.Equal(t, "input[4].content[0]", contentErr.Path)
	require.NotContains(t, err.Error(), "PRIVATE")
}

func TestRollInlineImagesIncludesAgentHistory(t *testing.T) {
	var input []any
	for i := range 3 {
		input = append(input, object{"type": "agent_message", "author": "/root/worker", "content": []any{
			object{"type": "input_text", "text": "agent screenshot"},
			object{"type": "input_image", "image_url": digestTestURL(t, i)},
		}})
	}
	raw, err := json.Marshal(object{"model": "gpt-6-astra", "input": input})
	require.NoError(t, err)
	out, rolled, err := RollInlineImages(context.Background(), raw, ImageWindow{Trigger: 2, Keep: 1, BatchCount: 10, BatchBytes: 32 << 20}, nil, "agent", func(_ context.Context, request []byte) (string, error) {
		require.Contains(t, string(request), "agent screenshot")
		return digestAnswer(t, request, "agent summary"), nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, rolled)
	require.Equal(t, 1, countInlineImages(out))
	require.Equal(t, "agent_message", gjson.GetBytes(out, "input.0.type").String())
	require.Contains(t, gjson.GetBytes(out, "input.0.content.1.text").String(), "agent summary")
}
