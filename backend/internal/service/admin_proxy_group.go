package service

import (
	"context"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *adminServiceImpl) proxyGroupRepository() (ProxyGroupRepository, error) {
	repo, ok := s.proxyRepo.(ProxyGroupRepository)
	if !ok {
		return nil, infraerrors.InternalServer("PROXY_GROUP_UNAVAILABLE", "proxy group repository unavailable")
	}
	return repo, nil
}

func normalizeProxyGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", infraerrors.BadRequest("PROXY_GROUP_NAME_INVALID", "proxy group name must contain 1 to 100 characters")
	}
	return name, nil
}

func (s *adminServiceImpl) ListProxyGroups(ctx context.Context) ([]ProxyGroup, error) {
	repo, err := s.proxyGroupRepository()
	if err != nil {
		return nil, err
	}
	return repo.ListProxyGroups(ctx)
}

func (s *adminServiceImpl) CreateProxyGroup(ctx context.Context, name string) (*ProxyGroup, error) {
	name, err := normalizeProxyGroupName(name)
	if err != nil {
		return nil, err
	}
	repo, err := s.proxyGroupRepository()
	if err != nil {
		return nil, err
	}
	return repo.CreateProxyGroup(ctx, name)
}

func (s *adminServiceImpl) UpdateProxyGroup(ctx context.Context, id int64, name string) (*ProxyGroup, error) {
	if id <= 0 {
		return nil, ErrProxyGroupNotFound
	}
	name, err := normalizeProxyGroupName(name)
	if err != nil {
		return nil, err
	}
	repo, err := s.proxyGroupRepository()
	if err != nil {
		return nil, err
	}
	return repo.UpdateProxyGroup(ctx, id, name)
}

func (s *adminServiceImpl) DeleteProxyGroup(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrProxyGroupNotFound
	}
	repo, err := s.proxyGroupRepository()
	if err != nil {
		return err
	}
	return repo.DeleteProxyGroup(ctx, id)
}

func (s *adminServiceImpl) validateProxyGroup(ctx context.Context, id *int64) error {
	if id == nil {
		return nil
	}
	if *id <= 0 {
		return infraerrors.BadRequest("PROXY_GROUP_ID_INVALID", "proxy group ID must be positive")
	}
	repo, err := s.proxyGroupRepository()
	if err != nil {
		return err
	}
	exists, err := repo.ProxyGroupExists(ctx, *id)
	if err != nil {
		return err
	}
	if !exists {
		return ErrProxyGroupNotFound
	}
	return nil
}

func (s *adminServiceImpl) AssignProxyGroup(ctx context.Context, ids []int64, groupID *int64) (int64, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return 0, infraerrors.BadRequest("PROXY_IDS_INVALID", "select between 1 and 1000 proxies")
	}
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return 0, infraerrors.BadRequest("PROXY_IDS_INVALID", "proxy IDs must be positive")
		}
		if !seen[id] {
			unique = append(unique, id)
			seen[id] = true
		}
	}
	if err := s.validateProxyGroup(ctx, groupID); err != nil {
		return 0, err
	}
	repo, err := s.proxyGroupRepository()
	if err != nil {
		return 0, err
	}
	return repo.AssignProxyGroup(ctx, unique, groupID)
}
