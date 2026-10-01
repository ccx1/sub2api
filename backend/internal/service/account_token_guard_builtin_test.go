package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAccountTokenGuardPreservesBuiltinConfiguration(t *testing.T) {
	cfg := defaultAccountTokenGuardConfig()
	cfg.Enabled, cfg.ActivationMode = true, AccountTokenGuardModeBuiltin
	cfg.ProbeEndpoint, cfg.ProbeModel, cfg.ReloginEndpoint = "", "", ""
	cfg.ReloginAccounts = []AccountTokenGuardReloginAccount{{Email: " GUARD1@example.com ",
		Password: "test-only", Disabled: true, ExpiresAt: 12345}}
	normalized := normalizeAccountTokenGuardConfig(cfg)
	if err := ValidateAccountTokenGuardConfig(normalized); err != nil {
		t.Fatal(err)
	}
	entry := normalized.ReloginAccounts[0]
	if normalized.ActivationMode != AccountTokenGuardModeBuiltin || !entry.Disabled || entry.ExpiresAt != 12345 || entry.Email != "guard1@example.com" {
		t.Fatal("local activation mode or credential lifecycle was lost")
	}
	normalized.ActivationMode = AccountTokenGuardModeExternal
	if ValidateAccountTokenGuardConfig(normalized) == nil {
		t.Fatal("external mode accepted missing endpoints")
	}
}

func TestAccountTokenGuardSkipsDisabledAndExpiredCredentials(t *testing.T) {
	accounts := &guardMemoryAccounts{items: []Account{guardTestAccount(1), guardTestAccount(2), guardTestAccount(3)}}
	svc := newGuardTestService(&guardMemoryRepo{}, accounts, "http://127.0.0.1:1")
	cfg := svc.currentConfig()
	cfg.ReloginAccounts = []AccountTokenGuardReloginAccount{
		{Email: accounts.items[0].Name, Disabled: true},
		{Email: accounts.items[1].Name, ExpiresAt: time.Now().Add(-time.Minute).Unix()},
	}
	selected, err := svc.listAccounts(context.Background(), cfg)
	if err != nil || len(selected) != 1 || selected[0].ID != 3 {
		t.Fatalf("selected=%v err=%v", selected, err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.reloginAccount(context.Background(), cfg, &accounts.items[i]); err == nil {
			t.Fatal("inactive credential entered relogin")
		}
	}
}

func TestAccountTokenGuardBuiltinCancellationStopsBeforeAuthorization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := &AccountTokenGuardService{}
	cfg := defaultAccountTokenGuardConfig()
	cfg.ActivationMode = AccountTokenGuardModeBuiltin
	account := guardTestAccount(1)
	result := svc.probeAccount(ctx, cfg, &account)
	if result.State != AccountTokenGuardProbeTransient || result.Diagnostic.Code != "request_canceled" {
		t.Fatalf("canceled builtin probe=%+v", result)
	}
	entry := AccountTokenGuardReloginAccount{Email: account.Name, Password: "test-only"}
	if _, err := svc.reloginWithMode(ctx, cfg, entry, &account); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled builtin relogin err=%v", err)
	}
	if _, err := svc.reloginBuiltin(ctx, cfg, entry, &account); !errors.Is(err, context.Canceled) {
		t.Fatalf("direct builtin relogin err=%v", err)
	}
}

func TestAccountTokenGuardBuiltinUsesRequestBudgets(t *testing.T) {
	account := guardRandomTestAccount(RandomProxyEmptyPoolPolicyReject)
	repo := &guardRuntimeProxyAccounts{guardMemoryAccounts: &guardMemoryAccounts{}, err: errors.New("proxy unavailable")}
	svc := &AccountTokenGuardService{accounts: repo}
	cfg := defaultAccountTokenGuardConfig()
	cfg.ProbeTimeoutSeconds = 7
	result := svc.probeBuiltin(context.Background(), cfg, &account)
	remaining := time.Until(repo.deadline)
	if result.State != AccountTokenGuardProbeTransient || remaining <= 0 || remaining > 7*time.Second {
		t.Fatalf("probe budget=%s result=%+v", remaining, result)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	entry := AccountTokenGuardReloginAccount{Email: account.Name, Password: "test-only"}
	if _, err := svc.reloginBuiltin(ctx, cfg, entry, &account); err == nil {
		t.Fatal("proxy failure unexpectedly entered authorization")
	}
	if remaining = time.Until(repo.deadline); remaining <= 0 || remaining > 40*time.Second {
		t.Fatalf("relogin did not inherit shorter parent budget: %s", remaining)
	}
}
