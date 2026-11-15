package kilo

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpa"
	concurrency "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

// ConnectionReader使用现有并发服务产生的连接引用，不维护第二套引用。
type ConnectionReader interface {
	List(context.Context) (concurrency.Snapshot, error)
}

type Service struct {
	resolver    concurrency.SetupResolver
	connections ConnectionReader
	client      *http.Client
}

func New(resolver concurrency.SetupResolver, connections ConnectionReader, client *http.Client) *Service {
	return &Service{resolver: resolver, connections: connections, client: client}
}

type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string                       { return e.Message }
func failure(status int, code, message string) error { return &Error{status, code, message} }

func (s *Service) setup(ctx context.Context) (store.Setup, error) {
	if s == nil || s.resolver == nil || s.connections == nil {
		return store.Setup{}, failure(503, "cpa_unavailable", "CPA连接暂不可用")
	}
	setup, ok, err := s.resolver.ResolveSetup(ctx)
	if err != nil || !ok || strings.TrimSpace(setup.CPAUpstreamURL) == "" || strings.TrimSpace(setup.ManagementKey) == "" {
		return store.Setup{}, failure(503, "cpa_unavailable", "CPA连接暂不可用")
	}
	return setup, nil
}

func sameConnection(a, b store.Setup) bool {
	return strings.TrimRight(a.CPAUpstreamURL, "/") == strings.TrimRight(b.CPAUpstreamURL, "/") && a.ManagementKey == b.ManagementKey
}

func (s *Service) boundSetup(ctx context.Context) (store.Setup, string, error) {
	setup, err := s.setup(ctx)
	if err != nil {
		return store.Setup{}, "", err
	}
	snapshot, err := s.connections.List(ctx)
	current, currentErr := s.setup(ctx)
	if currentErr != nil || !sameConnection(setup, current) {
		return store.Setup{}, "", failure(409, "cpa_target_changed", "CPA连接已变更，请重新查询")
	}
	if err != nil {
		var connectionErr *concurrency.Error
		if errors.As(err, &connectionErr) && connectionErr.Status == 409 {
			return store.Setup{}, "", failure(409, "cpa_target_changed", "CPA连接已变更，请重新查询")
		}
		return store.Setup{}, "", failure(503, "connection_reference_unavailable", "连接引用暂不可用")
	}
	if snapshot.Mode != "local" || snapshot.ConnectionRef == "" {
		return store.Setup{}, "", failure(503, "connection_reference_unavailable", "当前连接未提供本地管理引用")
	}
	return setup, snapshot.ConnectionRef, nil
}

func readBound[T any](ctx context.Context, s *Service, fetch func(context.Context, *http.Client, string, string) (T, error)) (T, string, error) {
	var empty T
	setup, reference, err := s.boundSetup(ctx)
	if err != nil {
		return empty, "", err
	}
	result, fetchErr := fetch(ctx, s.client, setup.CPAUpstreamURL, setup.ManagementKey)
	afterRead, err := s.setup(ctx)
	if err != nil {
		return empty, "", failure(409, "cpa_target_changed", "CPA连接已变更，请重新查询")
	}
	if !sameConnection(setup, afterRead) {
		return empty, "", failure(409, "cpa_target_changed", "CPA连接已变更，请重新查询")
	}
	if fetchErr != nil {
		if errors.Is(fetchErr, cpa.ErrKiloManagementAuth) {
			return empty, "", failure(502, "kilo_management_auth_failed", "Kilo管理连接认证失败")
		}
		return empty, "", failure(502, "kilo_read_failed", "Kilo管理读取未确认")
	}
	return result, reference, nil
}

func (s *Service) Accounts(ctx context.Context) (model.KiloAccounts, error) {
	result, ref, err := readBound(ctx, s, cpa.FetchKiloAccounts)
	result.ConnectionRef = ref
	for index := range result.Accounts {
		if result.Accounts[index].QuotaSnapshot != nil {
			result.Accounts[index].QuotaSnapshot.ConnectionRef = ref
		}
	}
	return result, err
}
func (s *Service) Status(ctx context.Context) (model.KiloStatus, error) {
	result, ref, err := readBound(ctx, s, cpa.FetchKiloStatus)
	result.ConnectionRef = ref
	return result, err
}
func (s *Service) Capabilities(ctx context.Context) (model.KiloCapabilities, error) {
	result, ref, err := readBound(ctx, s, cpa.FetchKiloCapabilities)
	result.ConnectionRef = ref
	return result, err
}

func HTTPError(err error) (int, string, string) {
	var target *Error
	if errors.As(err, &target) {
		return target.Status, target.Code, target.Message
	}
	return 502, "kilo_read_failed", "Kilo管理读取未确认"
}
