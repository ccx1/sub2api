package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketHarvestIntervalUsesConfiguredPollingInterval(t *testing.T) {
	protection := config.DefaultCodexTicketProtection()
	protection.RejectionRetryIntervalSeconds = 5
	protection.RejectionRetryCooldownSeconds = 10
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		HarvestProbeIntervalSeconds: 120,
		Protection:                  &protection,
	}, nil)

	require.Equal(t, 120*time.Second, svc.codexTicketHarvestInterval(context.Background()))
}
