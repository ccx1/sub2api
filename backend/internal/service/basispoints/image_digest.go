package basispoints

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrImageDigest = errors.New("basispoints could not describe earlier images")

const (
	imageDigestTTL        = 24 * time.Hour
	imageDigestMaxEntries = 4096
	imageDigestBatch      = 10
	imageDigestMaxText    = 8 << 10
	imageDigestContextMax = 1000
)

// ImageWindow bounds how many inline images a request keeps. Once more than
// Trigger remain, the oldest are described until at most Keep are left, so a
// growing conversation rolls in steps instead of describing on every turn.
type ImageWindow struct {
	Trigger    int
	Keep       int
	BatchCount int
	BatchBytes int
}

// ImageWindowForLimits leaves headroom below the per-request image limit and
// keeps each description request within the same count and size limits.
func ImageWindowForLimits(limits ImageRelayLimits) ImageWindow {
	maxImages := limits.MaxImages
	if maxImages < 1 {
		maxImages = imageRelayMaxRequestImages
	}
	trigger := max(maxImages-2, 1)
	batchBytes := imageRelayMaxRequestBytes
	if limits.MaxTotalMiB > 0 {
		batchBytes = min(batchBytes, limits.MaxTotalMiB<<20)
	}
	return ImageWindow{Trigger: trigger, Keep: max(trigger/2, 1), BatchCount: min(imageDigestBatch, maxImages), BatchBytes: batchBytes}
}

// DescribeImages sends one client request containing inline images and
// returns the final assistant text.
type DescribeImages func(ctx context.Context, request []byte) (string, error)

type inlineImageRef struct {
	part    object
	key     [32]byte
	size    int
	context string
	source  string
}

// RollInlineImages replaces older inline images with text descriptions when a
// request exceeds the window. Previously described images are reused from the
// cache; missing ones are described through describe in bounded batches. It
// returns the rewritten body and the number of images replaced by text.
func RollInlineImages(ctx context.Context, raw []byte, window ImageWindow, cache *ImageDigestCache, scope string, describe DescribeImages) ([]byte, int, error) {
	var source object
	if err := decode(raw, &source); err != nil || source == nil {
		return nil, 0, fmt.Errorf("invalid Basispoints request JSON")
	}
	refs := collectInlineImages(source)
	if len(refs) <= window.Trigger {
		return raw, 0, nil
	}
	descriptions := make(map[[32]byte]string)
	for _, ref := range refs {
		if _, ok := descriptions[ref.key]; ok {
			continue
		}
		if text, ok := cache.get(scope, ref.key); ok {
			descriptions[ref.key] = text
		}
	}
	// Count occurrences, since a repeated image is sent once per occurrence.
	counts := make(map[[32]byte]int)
	var pending []inlineImageRef
	remaining := 0
	for _, ref := range refs {
		if _, ok := descriptions[ref.key]; ok {
			continue
		}
		if counts[ref.key] == 0 {
			pending = append(pending, ref)
		}
		counts[ref.key]++
		remaining++
	}
	if remaining > window.Trigger {
		var todo []inlineImageRef
		for _, ref := range pending {
			if remaining <= window.Keep {
				break
			}
			todo = append(todo, ref)
			remaining -= counts[ref.key]
		}
		model := text(source["model"])
		for start := 0; start < len(todo); {
			end, size := start, 0
			for end < len(todo) && end-start < window.BatchCount && (end == start || size+todo[end].size <= window.BatchBytes) {
				size += todo[end].size
				end++
			}
			batch := todo[start:end]
			request, err := buildImageDigestRequest(model, batch)
			if err != nil {
				return nil, 0, err
			}
			answer, err := describe(ctx, request)
			if err != nil {
				return nil, 0, fmt.Errorf("%w: %w", ErrImageDigest, err)
			}
			texts, err := parseImageDigest(answer, len(batch))
			if err != nil {
				return nil, 0, fmt.Errorf("%w: %w", ErrImageDigest, err)
			}
			for i, ref := range batch {
				descriptions[ref.key] = texts[i]
				cache.put(scope, ref.key, texts[i])
			}
			start = end
		}
	}
	rolled := 0
	for _, ref := range refs {
		description, ok := descriptions[ref.key]
		if !ok {
			continue
		}
		for key := range ref.part {
			delete(ref.part, key)
		}
		ref.part["type"] = "input_text"
		ref.part["text"] = "[Earlier image replaced by the gateway: Basispoints accepts a limited number of images per request, so this image was converted into the following description generated from it.]\n" + description
		rolled++
	}
	if rolled == 0 {
		return raw, 0, nil
	}
	out, err := json.Marshal(source)
	if err != nil {
		return nil, 0, fmt.Errorf("encode basispoints image request")
	}
	return out, rolled, nil
}

// collectInlineImages visits the same typed content as PrepareNativeImages.
func collectInlineImages(source object) []inlineImageRef {
	var refs []inlineImageRef
	input, _ := source["input"].([]any)
	for index, entry := range input {
		item, _ := entry.(object)
		field, origin := "", ""
		switch text(item["type"]) {
		case "", "message", "agent_message":
			field, origin = "content", fmt.Sprintf("%s message at input[%d]", strings.TrimSpace(text(item["role"])), index)
		case "function_call_output", "custom_tool_call_output":
			field, origin = "output", fmt.Sprintf("tool output at input[%d]", index)
		default:
			continue
		}
		parts, _ := item[field].([]any)
		var context []string
		for _, value := range parts {
			part, _ := value.(object)
			switch text(part["type"]) {
			case "input_text", "output_text", "text":
				context = append(context, text(part["text"]))
			}
		}
		for _, value := range parts {
			part, _ := value.(object)
			if text(part["type"]) != "input_image" {
				continue
			}
			rawURL := text(part["image_url"])
			if !strings.HasPrefix(strings.ToLower(rawURL), "data:") {
				continue
			}
			refs = append(refs, inlineImageRef{
				part: part, key: sha256.Sum256([]byte(rawURL)), size: len(rawURL) / 4 * 3,
				context: truncateUTF8(strings.TrimSpace(strings.Join(context, "\n")), imageDigestContextMax),
				source:  strings.TrimSpace(origin),
			})
		}
	}
	return refs
}

func buildImageDigestRequest(model string, batch []inlineImageRef) ([]byte, error) {
	content := make([]any, 0, len(batch)*2+1)
	for i, ref := range batch {
		label := fmt.Sprintf("Image %d (from the %s).", i+1, ref.source)
		if ref.context != "" {
			label += " Text accompanying it in that message:\n" + ref.context
		}
		content = append(content, object{"type": "input_text", "text": label})
		image := object{"type": "input_image", "image_url": ref.part["image_url"]}
		if detail, ok := ref.part["detail"]; ok {
			image["detail"] = detail
		}
		content = append(content, image)
	}
	content = append(content, object{"type": "input_text", "text": fmt.Sprintf("Describe each of the %d images above, numbered 1 to %d.", len(batch), len(batch))})
	schema := object{
		"type": "object", "additionalProperties": false, "required": []any{"images"},
		"properties": object{"images": object{
			"type": "array",
			"items": object{
				"type": "object", "additionalProperties": false, "required": []any{"index", "description"},
				"properties": object{"index": object{"type": "integer"}, "description": object{"type": "string"}},
			},
		}},
	}
	return json.Marshal(object{
		"model": model, "tool_choice": "none", "reasoning": object{"effort": "low"},
		"instructions": "The following images come from an earlier part of a conversation. They will be removed from future requests and replaced by your descriptions, so the descriptions are the only record of them. " +
			"For each image, write a faithful, self-contained description: transcribe all legible text, code, numbers and table cells verbatim; describe charts, diagrams, UI state, layout, colors and notable details. " +
			"Do not speculate beyond what is visible and do not answer questions from the conversation. Write in the language of the accompanying text when there is any, otherwise English. Keep each description under 1500 words.",
		"text":  object{"format": object{"type": "json_schema", "name": "image_digest", "strict": true, "schema": schema}},
		"input": []any{object{"role": "user", "content": content}},
	})
}

func parseImageDigest(answer string, count int) ([]string, error) {
	answer = strings.TrimSpace(answer)
	if strings.HasPrefix(answer, "```") {
		answer = strings.TrimPrefix(strings.TrimPrefix(answer, "```json"), "```")
		answer = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(answer), "```"))
	}
	var result struct {
		Images []struct {
			Index       int    `json:"index"`
			Description string `json:"description"`
		} `json:"images"`
	}
	if err := json.Unmarshal([]byte(answer), &result); err != nil {
		return nil, fmt.Errorf("basispoints image description was not valid JSON")
	}
	texts := make([]string, count)
	for _, image := range result.Images {
		description := strings.TrimSpace(image.Description)
		if image.Index < 1 || image.Index > count || description == "" || texts[image.Index-1] != "" {
			return nil, fmt.Errorf("basispoints image description did not match the submitted images")
		}
		texts[image.Index-1] = truncateUTF8(description, imageDigestMaxText)
	}
	for _, description := range texts {
		if description == "" {
			return nil, fmt.Errorf("basispoints image description omitted a submitted image")
		}
	}
	return texts, nil
}

// ReadImageDigestResponse returns the final assistant text and the terminal
// usage (as a {"usage":...} JSON document, empty when absent).
func ReadImageDigestResponse(reader io.Reader) (string, []byte, error) {
	response, err := ReadToolRepairResponse(reader)
	var usage []byte
	if response != nil && response["usage"] != nil {
		usage, _ = json.Marshal(object{"usage": response["usage"]})
	}
	if err != nil {
		var failure *UpstreamFailure
		if errors.As(err, &failure) {
			return "", usage, fmt.Errorf("basispoints image description did not complete: %w", failure)
		}
		return "", usage, fmt.Errorf("basispoints image description did not complete")
	}
	var answer bytes.Buffer
	output, _ := response["output"].([]any)
	for _, raw := range output {
		item, _ := raw.(object)
		if text(item["type"]) != "message" {
			continue
		}
		content, _ := item["content"].([]any)
		for _, rawPart := range content {
			part, _ := rawPart.(object)
			if text(part["type"]) == "output_text" {
				answer.WriteString(text(part["text"]))
			}
		}
	}
	return answer.String(), usage, nil
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

type imageDigestEntry struct {
	text          string
	expires, used time.Time
}

// ImageDigestCache keeps descriptions (never images) for 24 hours, scoped by
// the caller and keyed by image content, bounded to 4096 entries.
type ImageDigestCache struct {
	mu      sync.Mutex
	entries map[[32]byte]imageDigestEntry
	now     func() time.Time
}

func (c *ImageDigestCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func imageDigestKey(scope string, image [32]byte) [32]byte {
	return sha256.Sum256([]byte(scope + "\x00" + string(image[:])))
}

func (c *ImageDigestCache) get(scope string, image [32]byte) (string, bool) {
	if c == nil {
		return "", false
	}
	key := imageDigestKey(scope, image)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	entry, ok := c.entries[key]
	if !ok {
		return "", false
	}
	if !now.Before(entry.expires) {
		delete(c.entries, key)
		return "", false
	}
	entry.used = now
	c.entries[key] = entry
	return entry.text, true
}

func (c *ImageDigestCache) put(scope string, image [32]byte, description string) {
	if c == nil {
		return
	}
	key := imageDigestKey(scope, image)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	if c.entries == nil {
		c.entries = make(map[[32]byte]imageDigestEntry)
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= imageDigestMaxEntries {
		var oldest [32]byte
		var used time.Time
		for k, entry := range c.entries {
			if !now.Before(entry.expires) {
				delete(c.entries, k)
				continue
			}
			if used.IsZero() || entry.used.Before(used) {
				oldest, used = k, entry.used
			}
		}
		if len(c.entries) >= imageDigestMaxEntries {
			delete(c.entries, oldest)
		}
	}
	c.entries[key] = imageDigestEntry{text: description, expires: now.Add(imageDigestTTL), used: now}
}
