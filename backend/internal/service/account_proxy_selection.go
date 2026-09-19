package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrRandomProxyUnavailable is returned when an account explicitly requests
// random proxy mode but the active proxy pool has no usable entry.
var ErrRandomProxyUnavailable = errors.New("random proxy pool has no active proxies")

type RandomProxyUnavailableError struct {
	Policy string
}

func (e *RandomProxyUnavailableError) Error() string {
	if e == nil || e.Policy == "" {
		return ErrRandomProxyUnavailable.Error()
	}
	return fmt.Sprintf("%s; policy=%s", ErrRandomProxyUnavailable.Error(), e.Policy)
}

func (e *RandomProxyUnavailableError) Unwrap() error {
	return ErrRandomProxyUnavailable
}

func RandomProxyUnavailablePolicy(err error) string {
	var unavailable *RandomProxyUnavailableError
	if errors.As(err, &unavailable) && unavailable != nil && unavailable.Policy != "" {
		return unavailable.Policy
	}
	return RandomProxyEmptyPoolPolicyReject
}

type randomProxyAccountDisabler interface {
	GetByID(ctx context.Context, id int64) (*Account, error)
	Update(ctx context.Context, account *Account) error
}

// DisableRandomProxyAccountOnUnavailable persists the "disable" empty-pool
// policy. Other policies are intentionally no-ops here: "reject" surfaces the
// request error and "direct" is handled by ResolveRandomProxy.
func DisableRandomProxyAccountOnUnavailable(ctx context.Context, account *Account, source any, cause error) error {
	if account == nil || !errors.Is(cause, ErrRandomProxyUnavailable) || RandomProxyUnavailablePolicy(cause) != RandomProxyEmptyPoolPolicyDisable {
		return nil
	}
	if repo, ok := source.(interface {
		DisableRandomProxyAccountIfUnavailable(context.Context, int64) error
	}); ok {
		return repo.DisableRandomProxyAccountIfUnavailable(ctx, account.ID)
	}
	repo, _ := source.(randomProxyAccountDisabler)
	if repo == nil {
		return nil
	}
	current, err := repo.GetByID(ctx, account.ID)
	if err != nil {
		return err
	}
	if current == nil || !current.IsRandomProxy() || current.RandomProxyEmptyPoolPolicy() != RandomProxyEmptyPoolPolicyDisable {
		return nil
	}
	current.Status = StatusDisabled
	current.Schedulable = false
	current.ErrorMessage = ErrRandomProxyUnavailable.Error()
	current.ProxyID = nil
	current.Proxy = nil
	if current.Extra != nil {
		current.Extra = NormalizeProxyModeExtra(current.Extra)
	}
	return repo.Update(ctx, current)
}

func randomProxyUnavailable(account *Account) error {
	return &RandomProxyUnavailableError{Policy: account.RandomProxyEmptyPoolPolicy()}
}

// ResolveRandomProxy resolves the account's preferred healthy proxy. The
// assignment is runtime-only; the allocator retains affinity across requests
// without persisting a fixed proxy_id on the account.
func ResolveRandomProxy(ctx context.Context, account *Account, selector RandomProxySelector) error {
	if account == nil || !account.IsRandomProxy() {
		return nil
	}
	// Clear any association inherited from a stale snapshot before querying the
	// current pool. If selection fails, callers must not accidentally route
	// through an old fixed proxy unless the configured policy explicitly allows
	// direct egress.
	account.ProxyID = nil
	account.Proxy = nil
	if selector == nil {
		if account.RandomProxyEmptyPoolPolicy() == RandomProxyEmptyPoolPolicyDirect {
			return nil
		}
		return randomProxyUnavailable(account)
	}
	proxy, err := selectAccountRandomProxy(ctx, account, selector)
	if err != nil {
		return fmt.Errorf("select random account proxy: %w", err)
	}
	if proxy == nil || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
		if account.RandomProxyEmptyPoolPolicy() == RandomProxyEmptyPoolPolicyDirect {
			return nil
		}
		return randomProxyUnavailable(account)
	}
	proxyID := proxy.ID
	account.ProxyID = &proxyID
	account.Proxy = proxy
	return nil
}

// ResolveRandomProxyFromSource is a convenience adapter for services whose
// existing dependency is an AccountRepository interface. Implementations that
// support random proxy selection opt in via the optional interface above.
func ResolveRandomProxyFromSource(ctx context.Context, account *Account, source any) error {
	if account == nil || !account.IsRandomProxy() {
		return nil
	}
	selector, _ := source.(RandomProxySelector)
	return ResolveRandomProxy(ctx, account, selector)
}
