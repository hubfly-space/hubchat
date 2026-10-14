package events

import "context"

type actionContextKey struct{}

// WithActionKey carries the durable identity of an automation action to domain
// adapters so a replay cannot create another message, task, or queued delivery.
func WithActionKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, actionContextKey{}, key)
}
func ActionKey(ctx context.Context) string {
	key, _ := ctx.Value(actionContextKey{}).(string)
	return key
}

type causationContextKey struct{}

func WithCausation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, causationContextKey{}, id)
}
func CausationID(ctx context.Context) string {
	id, _ := ctx.Value(causationContextKey{}).(string)
	return id
}
