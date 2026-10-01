package basispoints

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrInlineImage marks base64 input that the caller may rewrite through ImageRelay.
var ErrInlineImage = errors.New("basispoints does not accept data:image/base64 image input while image support is disabled; provide an HTTPS image URL, or disable Basispoints and start a new conversation to send this image")

// Accept HTTPS URLs or validated native attachment references.
func validateImage(part object) error {
	if fileID, exists := part["file_id"]; exists {
		id, ok := fileID.(string)
		if !ok || !ValidAttachmentID(id) {
			return fmt.Errorf("basispoints input_image requires a valid file_id")
		}
		if _, exists := part["image_url"]; exists {
			return fmt.Errorf("basispoints input_image requires exactly one image reference")
		}
	} else {
		raw, ok := part["image_url"].(string)
		if !ok || raw == "" {
			return fmt.Errorf("basispoints input_image requires an HTTPS image_url or file_id")
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "data:") {
			return ErrInlineImage
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || strings.TrimSpace(raw) != raw {
			return fmt.Errorf("basispoints input_image requires an absolute HTTPS image URL without embedded credentials")
		}
	}
	return validateImageDetail(part)
}

func validateImageDetail(part object) error {
	if detail, exists := part["detail"]; exists && detail != nil {
		switch text(detail) {
		case "auto", "low", "high", "original":
		default:
			return fmt.Errorf("basispoints image detail must be auto, low, high or original")
		}
	}
	return nil
}
