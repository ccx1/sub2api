package basispoints

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Each index produces a distinct valid PNG.
func digestTestURL(t *testing.T, index int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: uint8(index), G: uint8(index >> 8), A: 255})
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, img))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())
}

// One user message per image, as in a long screenshot conversation.
func digestTestRequest(t *testing.T, urls ...string) []byte {
	t.Helper()
	var input []any
	for i, url := range urls {
		input = append(input, object{"role": "user", "content": []any{
			object{"type": "input_text", "text": fmt.Sprintf("screenshot %d", i)},
			object{"type": "input_image", "image_url": url, "detail": "high"},
		}})
		input = append(input, object{"type": "message", "role": "assistant", "content": []any{object{"type": "output_text", "text": "ok"}}})
	}
	raw, err := json.Marshal(object{"model": "gpt-6-astra", "input": input})
	require.NoError(t, err)
	return raw
}

func digestAnswer(t *testing.T, request []byte, prefix string) string {
	t.Helper()
	var images []any
	for _, part := range gjson.GetBytes(request, "input.0.content").Array() {
		if part.Get("type").String() == "input_image" {
			images = append(images, object{"index": len(images) + 1, "description": fmt.Sprintf("%s %d", prefix, len(images)+1)})
		}
	}
	raw, err := json.Marshal(object{"images": images})
	require.NoError(t, err)
	return string(raw)
}

func countInlineImages(raw []byte) int {
	var source object
	_ = json.Unmarshal(raw, &source)
	return len(collectInlineImages(source))
}

func TestRollInlineImagesUnderWindowIsUntouched(t *testing.T) {
	var urls []string
	for i := 0; i < 18; i++ {
		urls = append(urls, digestTestURL(t, i))
	}
	raw := digestTestRequest(t, urls...)
	out, rolled, err := RollInlineImages(context.Background(), raw, ImageWindowForLimits(DefaultImageRelayLimits()), &ImageDigestCache{}, "scope", func(context.Context, []byte) (string, error) {
		t.Fatal("no description expected under the window")
		return "", nil
	})
	require.NoError(t, err)
	require.Zero(t, rolled)
	require.Equal(t, raw, out)
}

func TestRollInlineImagesDescribesOldestAndKeepsRecent(t *testing.T) {
	var urls []string
	for i := 0; i < 25; i++ {
		urls = append(urls, digestTestURL(t, i))
	}
	raw := digestTestRequest(t, urls...)
	window := ImageWindowForLimits(DefaultImageRelayLimits())
	require.Equal(t, ImageWindow{Trigger: 18, Keep: 9, BatchCount: 10, BatchBytes: 32 << 20}, window)
	var batches []int
	out, rolled, err := RollInlineImages(context.Background(), raw, window, &ImageDigestCache{}, "scope", func(_ context.Context, request []byte) (string, error) {
		require.Equal(t, "gpt-6-astra", gjson.GetBytes(request, "model").String())
		require.Equal(t, "none", gjson.GetBytes(request, "tool_choice").String())
		require.Equal(t, "json_schema", gjson.GetBytes(request, "text.format.type").String())
		require.Contains(t, gjson.GetBytes(request, "input.0.content.0.text").String(), "screenshot")
		require.Equal(t, "high", gjson.GetBytes(request, "input.0.content.1.detail").String())
		batches = append(batches, countInlineImages(request))
		return digestAnswer(t, request, fmt.Sprintf("batch%d", len(batches))), nil
	})
	require.NoError(t, err)
	require.Equal(t, 16, rolled)
	require.Equal(t, []int{10, 6}, batches)
	require.Equal(t, 9, countInlineImages(out))
	input := gjson.GetBytes(out, "input").Array()
	// The oldest image became text; the newest stays an image.
	require.Equal(t, "input_text", input[0].Get("content.1.type").String())
	require.Contains(t, input[0].Get("content.1.text").String(), "batch1 1")
	require.Contains(t, input[30].Get("content.1.text").String(), "batch2 6")
	require.Equal(t, urls[24], input[48].Get("content.1.image_url").String())
	require.Equal(t, urls[16], input[32].Get("content.1.image_url").String())
	_, _, err = Prepare(out, "scope", nil)
	require.ErrorIs(t, err, ErrInlineImage, "remaining recent images still go through the configured image mode")
}

func TestRollInlineImagesReusesCachedDescriptions(t *testing.T) {
	cache := &ImageDigestCache{}
	window := ImageWindowForLimits(DefaultImageRelayLimits())
	var urls []string
	for i := 0; i < 19; i++ {
		urls = append(urls, digestTestURL(t, i))
	}
	calls := 0
	describe := func(_ context.Context, request []byte) (string, error) {
		calls++
		return digestAnswer(t, request, "cached"), nil
	}
	out, rolled, err := RollInlineImages(context.Background(), digestTestRequest(t, urls...), window, cache, "scope", describe)
	require.NoError(t, err)
	require.Equal(t, 10, rolled)
	require.Equal(t, 9, countInlineImages(out))
	require.Equal(t, 1, calls)

	// The next turn adds images: known ones reuse text, new ones stay images
	// until the remaining count exceeds the window again.
	for i := 19; i < 27; i++ {
		urls = append(urls, digestTestURL(t, i))
	}
	out, rolled, err = RollInlineImages(context.Background(), digestTestRequest(t, urls...), window, cache, "scope", describe)
	require.NoError(t, err)
	require.Equal(t, 10, rolled)
	require.Equal(t, 17, countInlineImages(out))
	require.Equal(t, 1, calls)

	// A different scope never sees those descriptions.
	_, _, err = RollInlineImages(context.Background(), digestTestRequest(t, urls...), window, cache, "other", describe)
	require.NoError(t, err)
	require.Equal(t, 3, calls)
}

func TestRollInlineImagesCountsRepeatedImages(t *testing.T) {
	same := digestTestURL(t, 1)
	var urls []string
	for i := 0; i < 20; i++ {
		urls = append(urls, same)
	}
	urls = append(urls, digestTestURL(t, 2))
	var described int
	out, rolled, err := RollInlineImages(context.Background(), digestTestRequest(t, urls...), ImageWindowForLimits(DefaultImageRelayLimits()), nil, "", func(_ context.Context, request []byte) (string, error) {
		described += countInlineImages(request)
		return digestAnswer(t, request, "same"), nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, described, "a repeated image is described once")
	require.Equal(t, 20, rolled)
	require.Equal(t, 1, countInlineImages(out))
}

func TestRollInlineImagesFailsClosed(t *testing.T) {
	var urls []string
	for i := 0; i < 21; i++ {
		urls = append(urls, digestTestURL(t, i))
	}
	raw := digestTestRequest(t, urls...)
	window := ImageWindowForLimits(DefaultImageRelayLimits())
	for name, describe := range map[string]DescribeImages{
		"upstream": func(context.Context, []byte) (string, error) { return "", errors.New("PRIVATE_UPSTREAM") },
		"not json": func(context.Context, []byte) (string, error) { return "cannot see images", nil },
		"missing": func(context.Context, []byte) (string, error) {
			return `{"images":[{"index":1,"description":"x"}]}`, nil
		},
		"bad index": func(_ context.Context, request []byte) (string, error) {
			return strings.Replace(digestAnswer(t, request, "x"), `"index":1}`, `"index":99}`, 1), nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			cache := &ImageDigestCache{}
			_, _, err := RollInlineImages(context.Background(), raw, window, cache, "scope", describe)
			require.ErrorIs(t, err, ErrImageDigest)
			require.Empty(t, cache.entries)
		})
	}
}

func TestRollInlineImagesSmallConfiguredLimit(t *testing.T) {
	limits := DefaultImageRelayLimits()
	limits.MaxImages = 1
	window := ImageWindowForLimits(limits)
	require.Equal(t, ImageWindow{Trigger: 1, Keep: 1, BatchCount: 1, BatchBytes: 32 << 20}, window)
	out, rolled, err := RollInlineImages(context.Background(), digestTestRequest(t, digestTestURL(t, 1), digestTestURL(t, 2), digestTestURL(t, 3)), window, nil, "", func(_ context.Context, request []byte) (string, error) {
		require.Equal(t, 1, countInlineImages(request))
		return digestAnswer(t, request, "one"), nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, rolled)
	require.Equal(t, 1, countInlineImages(out))
}

func TestReadImageDigestResponse(t *testing.T) {
	wire := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"{\\\"images\\\":[]}\"}]}],\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}}\n\n"
	answer, usage, err := ReadImageDigestResponse(strings.NewReader(wire))
	require.NoError(t, err)
	require.Equal(t, `{"images":[]}`, answer)
	require.Equal(t, int64(7), gjson.GetBytes(usage, "usage.input_tokens").Int())

	_, _, err = ReadImageDigestResponse(strings.NewReader("event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\"}}\n\n"))
	require.Error(t, err)
}

func TestImageDigestPromptPassesPrepare(t *testing.T) {
	request, err := buildImageDigestRequest("gpt-6-astra", []inlineImageRef{{part: object{"type": "input_image", "image_url": "https://images.example/a.png"}, source: "user message at input[0]"}})
	require.NoError(t, err)
	body, bridge, err := Prepare(request, "scope", nil)
	require.NoError(t, err)
	require.NotNil(t, bridge)
	require.Equal(t, "low", gjson.GetBytes(body, "reasoning_effort").String())
	require.Contains(t, string(body), "https://images.example/a.png")
}
