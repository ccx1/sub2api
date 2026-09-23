package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
)

func (s *OpenAIGatewayService) openAICodexTicketProbeSessionID(ctx context.Context, in openAICodexTicketProbeInput) (string, error) {
	if in.SessionID != nil && strings.TrimSpace(*in.SessionID) != "" {
		return strings.TrimSpace(*in.SessionID), nil
	}
	cfg := in.Config
	if cfg == nil {
		runtime := s.openAICodexTicketConfigContext(ctx)
		cfg = &runtime
	}
	if cfg.SessionMode == "" || cfg.SessionMode == config.CodexTicketSessionRandom {
		return uuid.NewString(), nil
	}
	if in.Account == nil || in.Account.ID <= 0 {
		return "", errors.New("codex ticket session account unavailable")
	}
	key := strconv.FormatInt(in.Account.ID, 10)
	switch cfg.SessionMode {
	case config.CodexTicketSessionAccount:
	case config.CodexTicketSessionAccountModel:
		key = openAICodexTicketKey(in.Account.ID, normalizeOpenAICodexTicketModel(in.Model))
	default:
		return "", errors.New("invalid codex ticket session mode")
	}
	epoch, err := s.openAICodexTicketSessionEpoch(ctx, in, cfg.SessionMode)
	if err != nil {
		return "", err
	}
	// 轮次只由共享调度在阈值处推进；本次采集与复验使用同一个快照。
	name := "sub2api/codex-ticket/session/v1/" + cfg.SessionMode + "/" + key + "/" + epoch
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(name)).String(), nil
}

func (s *OpenAIGatewayService) openAICodexTicketSessionEpoch(ctx context.Context, in openAICodexTicketProbeInput, mode string) (string, error) {
	if in.SessionEpoch != nil {
		return *in.SessionEpoch, nil
	}
	if s != nil {
		if reader, ok := s.accountRepo.(CodexTicketSessionEpochReader); ok {
			return reader.GetCodexTicketSessionEpoch(ctx, CodexTicketSessionScope{AccountID: in.Account.ID, Model: normalizeOpenAICodexTicketModel(in.Model), Mode: mode})
		}
	}
	return "", nil
}
