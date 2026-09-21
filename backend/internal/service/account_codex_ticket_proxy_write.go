package service

import "context"

type codexTicketProxyWriteKey struct{}
type codexTicketProxyWriteFields struct{ mode, id bool }

// 仅账号管理请求可显式更改配置；后台刷新整对象时不得回写读取时的旧配置。
func WithCodexTicketProxyWrite(ctx context.Context, originalExtra map[string]any) context.Context {
	_, mode := originalExtra[CodexTicketProxyModeExtraKey]
	_, id := originalExtra[CodexTicketProxyIDExtraKey]
	return context.WithValue(ctx, codexTicketProxyWriteKey{}, codexTicketProxyWriteFields{mode: mode, id: id})
}

func CodexTicketProxyWriteFields(ctx context.Context) (mode bool, id bool) {
	fields, _ := ctx.Value(codexTicketProxyWriteKey{}).(codexTicketProxyWriteFields)
	return fields.mode, fields.id
}
