package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedPoolAsyncMediaBlockedBeforeUpstream(t *testing.T) {
	account := &Account{Platform: PlatformGrok, Extra: map[string]any{"shared_pool_owner_id": int64(6)}}
	var gateway *OpenAIGatewayService
	for _, endpoint := range []GrokMediaEndpoint{GrokMediaEndpointVideosGenerations, GrokMediaEndpointVideosEdits,
		GrokMediaEndpointVideosExtensions, GrokMediaEndpointVideoStatus, GrokMediaEndpointVideoContent} {
		_, err := gateway.ForwardGrokMedia(context.Background(), nil, account, endpoint, "", nil, "")
		require.ErrorIs(t, err, ErrSharedPoolAsyncMediaUnsupported)
	}
	for _, endpoint := range []GrokMediaEndpoint{SeedanceEndpointCreate, SeedanceEndpointStatus, SeedanceEndpointDelete} {
		_, err := gateway.ForwardSeedance(context.Background(), nil, account, endpoint, "", nil)
		require.ErrorIs(t, err, ErrSharedPoolAsyncMediaUnsupported)
	}
}

func TestSharedPoolSynchronousMediaRemainsEligible(t *testing.T) {
	account := &Account{Extra: map[string]any{"shared_pool_owner_id": int64(6)}}
	for _, endpoint := range []GrokMediaEndpoint{GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits} {
		require.NoError(t, validateSharedPoolMediaBilling(account, endpoint))
	}
	require.NoError(t, validateSharedPoolMediaBilling(&Account{}, GrokMediaEndpointVideosGenerations))
}
