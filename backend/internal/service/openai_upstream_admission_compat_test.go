//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAITransportErrorPreservesLocalAdmissionRejection(t *testing.T) {
	denied := &OpenAITurnAdmissionError{Reason: "account_ineligible"}
	for _, input := range []error{denied, fmt.Errorf("transport: %w", denied)} {
		c := newTransportErrorTestGin(t)
		err := (&OpenAIGatewayService{}).handleOpenAIUpstreamTransportError(
			context.Background(), c, &Account{ID: 1, Platform: PlatformOpenAI}, input, false)
		require.ErrorIs(t, err, denied)
		_, recorded := c.Get(OpsUpstreamErrorsKey)
		require.False(t, recorded, "本地准入拒绝不得记为上游故障")
		_, marked := c.Get(OpsUpstreamErrorMessageKey)
		require.False(t, marked)
	}
}
