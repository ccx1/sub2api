package service

import (
	"context"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrAPIKeySpendGuardFrozen = infraerrors.Forbidden("API_KEY_SPEND_GUARD_FROZEN", "API key is frozen by spend guard; contact an administrator to unfreeze it")

type apiKeySpendGuardStateReader interface {
	IsAPIKeySpendGuardFrozen(context.Context, int64) (bool, error)
}

func (s *APIKeyService) checkSpendGuardReactivation(ctx context.Context, id int64) error {
	reader, ok := s.apiKeyRepo.(apiKeySpendGuardStateReader)
	if !ok {
		return nil
	}
	frozen, err := reader.IsAPIKeySpendGuardFrozen(ctx, id)
	if err != nil {
		return fmt.Errorf("check API key spend guard freeze: %w", err)
	}
	if frozen {
		return ErrAPIKeySpendGuardFrozen
	}
	return nil
}
