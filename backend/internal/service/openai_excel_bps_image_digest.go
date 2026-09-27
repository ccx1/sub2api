package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

var excelBPSImageDigests basispoints.ImageDigestCache

// rollExcelBPSImages keeps a request under the BPS inline image limit by
// replacing the oldest images with descriptions produced on the same account,
// model, proxy and image mode. Descriptions (never images) are cached per API
// key, so later turns reuse them even after an account switch.
func (s *OpenAIGatewayService) rollExcelBPSImages(ctx context.Context, c *gin.Context, account *Account, body []byte, scope string, settings ExcelBPSImageRelaySettings, usage *OpenAIUsage) ([]byte, error) {
	var cache *basispoints.ImageDigestCache
	cacheScope := ""
	if apiKeyID := getAPIKeyIDFromContext(c); apiKeyID != 0 {
		cache, cacheScope = &excelBPSImageDigests, fmt.Sprintf("key:%d", apiKeyID)
	}
	window := basispoints.ImageWindowForLimits(settings.Limits)
	rolled, count, err := basispoints.RollInlineImages(ctx, body, window, cache, cacheScope, func(describeCtx context.Context, request []byte) (string, error) {
		return s.describeExcelBPSImages(describeCtx, account, request, scope+"/image-digest", settings, usage)
	})
	if err == nil && count > 0 {
		logger.FromContext(ctx).Info("excel_bps.images_rolled", zap.Int64("account_id", account.ID), zap.Int("replaced", count), zap.Int("window", window.Trigger))
	}
	return rolled, err
}

func (s *OpenAIGatewayService) describeExcelBPSImages(ctx context.Context, account *Account, request []byte, scope string, settings ExcelBPSImageRelaySettings, usage *OpenAIUsage) (string, error) {
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return "", err
	}
	accountID := excelBPSAccountID(account, token)
	if accountID == "" {
		return "", fmt.Errorf("excel BPS requires chatgpt_account_id")
	}
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if settings.Mode == ExcelBPSImageModeNative {
		images, err := basispoints.PrepareNativeImagesWithLimit(request, settings.Limits.MaxImages)
		if err != nil {
			return "", err
		}
		request, err = images.Body()
		if err == nil {
			_, _, err = basispoints.Prepare(request, scope, nil)
		}
		if err != nil {
			return "", err
		}
		ctx = context.WithValue(ctx, excelBPSPinnedEgressKey{}, true)
		// Described images are cached as text, so their file IDs are not reused.
		request, err = images.Upload(ctx, &s.excelBPSAttachments, "", func(uploadCtx context.Context, img basispoints.InlineAttachment) (string, error) {
			return s.uploadExcelBPSAttachment(uploadCtx, account, token, accountID, proxyURL, img)
		})
		if err != nil {
			return "", err
		}
	} else {
		relay, err := s.excelBPSImageRelayForSettings(settings)
		if err != nil {
			return "", err
		}
		if relay == nil {
			return "", basispoints.ErrInlineImage
		}
		request, err = relay.Rewrite(request, scope)
		if err != nil {
			return "", err
		}
	}
	upstreamBody, _, err := basispoints.Prepare(request, scope, nil)
	if err != nil {
		return "", err
	}
	req, err := newExcelBPSRequest(WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileLongStream)), upstreamBody, token, accountID)
	if err != nil {
		return "", err
	}
	resp, err := s.doExcelBPSUpstream(req, proxyURL, account)
	if err != nil {
		return "", fmt.Errorf("excel BPS image description connection failed")
	}
	// Release the account's connection slot before the main request.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
		s.handleExcelBPSUnauthorized(ctx, account, resp.StatusCode, resp.Header, raw)
		if resp.StatusCode == http.StatusForbidden && gjson.GetBytes(raw, "error.code").String() != "basispoints_model_access_changed" {
			s.moveExcelBPSOn403(ctx, account)
			s.disableExcelBPSOn403(ctx, account)
		}
		return "", &excelBPSAttachmentError{status: resp.StatusCode}
	}
	s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, resp.Header)
	answer, reported, err := basispoints.ReadImageDigestResponse(resp.Body)
	if parsed, ok := extractOpenAIUsageFromJSONBytes(reported); ok {
		addOpenAIUsage(usage, parsed)
	}
	return answer, err
}

// excelBPSImageDigestFailure maps a rolling failure to the client response.
func excelBPSImageDigestFailure(err error) (int, string, string) {
	switch {
	case errors.Is(err, basispoints.ErrImageRelayFull):
		return http.StatusServiceUnavailable, "basispoints_image_relay_full", err.Error()
	case errors.Is(err, basispoints.ErrImageRelayStorage), errors.Is(err, basispoints.ErrAttachmentBusy):
		return http.StatusServiceUnavailable, "basispoints_image_relay_unavailable", "Excel BPS image storage is unavailable"
	}
	var upstream *excelBPSAttachmentError
	if errors.As(err, &upstream) {
		switch upstream.status {
		case http.StatusTooManyRequests:
			return upstream.status, "basispoints_rate_limited", "Excel BPS rate limit exceeded while describing earlier images; request was not sent"
		case http.StatusUnauthorized, http.StatusForbidden:
			return upstream.status, "basispoints_upstream_error", "Excel BPS rejected the request that describes earlier images; request was not sent"
		}
	}
	return http.StatusBadGateway, "basispoints_image_digest_failed", "Excel BPS could not describe earlier images to stay within its per-request image limit; request was not sent"
}
