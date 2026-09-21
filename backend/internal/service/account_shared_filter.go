package service

import "context"

type sharedAccountFilterKey struct{}

func WithSharedAccountFilter(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, sharedAccountFilterKey{}, value)
}

func SharedAccountFilter(ctx context.Context) string {
	value, _ := ctx.Value(sharedAccountFilterKey{}).(string)
	return value
}
