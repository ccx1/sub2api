package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// checkGroupSecurityPolicy is the single gateway entry point for group
// security policies. Disabled groups are a strict no-op.
func checkGroupSecurityPolicy(
	c *gin.Context,
	securityPolicy *service.SecurityPolicyService,
	apiKey *service.APIKey,
	protocol, model string,
	body []byte,
) *securityaudit.Decision {
	if securityPolicy == nil || apiKey == nil {
		return nil
	}
	group := apiKey.Group
	if group == nil || !group.SecurityPolicyEnabled {
		return nil
	}

	mode := service.NormalizeSecurityPolicyMode(group.SecurityPolicyMode)
	verdict := securityPolicy.EvaluateRequest(c.Request.Context(), c, service.SecurityPolicyRequest{
		APIKey:    apiKey,
		Protocol:  protocol,
		Model:     model,
		Endpoint:  GetInboundEndpoint(c),
		Body:      body,
		RequestID: c.Writer.Header().Get("X-Request-Id"),
		ClientIP:  strings.TrimSpace(ip.GetClientIP(c)),
		UserAgent: c.GetHeader("User-Agent"),
	})
	if verdict == nil || verdict.Allowed {
		return nil
	}

	record := service.SecurityPolicyHitRecord{
		RequestID:   c.Writer.Header().Get("X-Request-Id"),
		APIKey:      apiKey,
		Protocol:    protocol,
		Model:       model,
		Endpoint:    GetInboundEndpoint(c),
		Verdict:     verdict,
		SessionMode: mode,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		securityPolicy.RecordHit(ctx, record)
	}()

	return &securityaudit.Decision{
		Kind:           securityaudit.DecisionBlock,
		HTTPStatus:     http.StatusForbidden,
		ErrorCode:      verdict.ErrorCode,
		ClientMessage:  verdict.ClientMessage,
		AllowNextStage: false,
	}
}
