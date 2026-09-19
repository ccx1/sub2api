package service

import "github.com/Wei-Shaw/sub2api/internal/config"

func ProvideTLSFingerprintProfileService(repo TLSFingerprintProfileRepository, cache TLSFingerprintProfileCache, cfg *config.Config) *TLSFingerprintProfileService {
	svc := NewTLSFingerprintProfileService(repo, cache)
	svc.SetConfig(cfg)
	return svc
}
