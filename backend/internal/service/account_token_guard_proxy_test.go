package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

type guardRuntimeProxyAccounts struct {
	*guardMemoryAccounts
	proxy      *Proxy
	err        error
	disableErr error
	disabled   []int64
	deadline   time.Time
}

func (r *guardRuntimeProxyAccounts) SelectRandomActiveProxy(ctx context.Context) (*Proxy, error) {
	r.deadline, _ = ctx.Deadline()
	return r.proxy, r.err
}

func (r *guardRuntimeProxyAccounts) DisableRandomProxyAccountIfUnavailable(_ context.Context, id int64) error {
	r.disabled = append(r.disabled, id)
	return r.disableErr
}

func (r *guardRuntimeProxyAccounts) ResolveFixedProxyFailover(context.Context, *Account) (*Proxy, error) {
	return r.proxy, r.err
}

type guardFixedProxyRepo struct {
	ProxyRepository
	proxy *Proxy
	err   error
}

func (r *guardFixedProxyRepo) GetByID(context.Context, int64) (*Proxy, error) {
	return r.proxy, r.err
}

func guardRandomTestAccount(policy string) Account {
	account := guardTestAccount(1)
	account.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom, RandomProxyEmptyPoolPolicyExtraKey: policy}
	return account
}

func TestAccountTokenGuardRandomProxyOnlyChangesRequestCopy(t *testing.T) {
	account := guardRandomTestAccount(RandomProxyEmptyPoolPolicyReject)
	proxy := &Proxy{ID: 99, Protocol: "http", Host: "127.0.0.1", Port: 8080, Status: StatusActive}
	repo := &guardRuntimeProxyAccounts{guardMemoryAccounts: &guardMemoryAccounts{}, proxy: proxy}
	svc := &AccountTokenGuardService{accounts: repo}
	url, err := svc.resolveAccountProxyURL(context.Background(), &account)
	if err != nil || url != proxy.URL() || account.ProxyID != nil || account.Proxy != nil {
		t.Fatalf("runtime proxy=%q err=%v original proxy=%v", url, err, account.ProxyID)
	}
}

func TestAccountTokenGuardEmptyProxyPoolPolicies(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable, RandomProxyEmptyPoolPolicyDirect} {
		t.Run(policy, func(t *testing.T) {
			account := guardRandomTestAccount(policy)
			repo := &guardRuntimeProxyAccounts{guardMemoryAccounts: &guardMemoryAccounts{}}
			svc := &AccountTokenGuardService{accounts: repo}
			url, err := svc.resolveAccountProxyURL(context.Background(), &account)
			if policy == RandomProxyEmptyPoolPolicyDirect {
				if err != nil || url != "" {
					t.Fatalf("direct policy url=%q err=%v", url, err)
				}
			} else if !errors.Is(err, ErrRandomProxyUnavailable) {
				t.Fatalf("pool failure allowed direct egress: %v", err)
			}
			if (len(repo.disabled) == 1) != (policy == RandomProxyEmptyPoolPolicyDisable) {
				t.Fatalf("policy=%s disabled=%v", policy, repo.disabled)
			}
		})
	}
}

func TestAccountTokenGuardFixedProxyFailureDoesNotFallBackToDirect(t *testing.T) {
	account := guardTestAccount(1)
	id := int64(7)
	account.ProxyID = &id
	wantErr := errors.New("proxy lookup failed")
	svc := &AccountTokenGuardService{accounts: &guardMemoryAccounts{}, proxyRepo: &guardFixedProxyRepo{err: wantErr}}
	if _, err := svc.resolveAccountProxyURL(context.Background(), &account); !errors.Is(err, wantErr) {
		t.Fatalf("lookup error was hidden: %v", err)
	}
	svc.proxyRepo = &guardFixedProxyRepo{}
	if _, err := svc.resolveAccountProxyURL(context.Background(), &account); !errors.Is(err, ErrRandomProxyUnavailable) {
		t.Fatalf("missing proxy was hidden: %v", err)
	}
}

func TestAccountTokenGuardPreservesSourceSelectedFixedFallback(t *testing.T) {
	account := guardTestAccount(1)
	original := &Proxy{ID: 7, Protocol: "http", Host: "127.0.0.1", Port: 8080, Status: StatusActive}
	selected := &Proxy{ID: 8, Protocol: "http", Host: "127.0.0.1", Port: 8081, Status: StatusActive}
	account.ProxyID, account.Proxy = &original.ID, original
	repo := &guardRuntimeProxyAccounts{guardMemoryAccounts: &guardMemoryAccounts{}, proxy: selected}
	svc := &AccountTokenGuardService{accounts: repo}
	url, err := svc.resolveAccountProxyURL(context.Background(), &account)
	if err != nil || url != selected.URL() || *account.ProxyID != original.ID || account.Proxy != original {
		t.Fatalf("fallback url=%q err=%v original=%+v", url, err, account.Proxy)
	}
}
