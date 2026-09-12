package provider

import (
	"context"
	"strings"
)

type streamSessionKey struct{}

// WithStreamSession binds the conversation identity that official-route
// adapters replay as x-opencode-session. The OpenCode Go gateway rejects
// header-less requests with 400 and keys prompt-cache affinity on this value,
// so callers must bind one stable id per conversation (Reasonix uses the
// session-path branch id) and a different id per conversation. An empty id
// binds nothing: fabricating a value would silently break cache isolation.
func WithStreamSession(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, streamSessionKey{}, sessionID)
}

// StreamSessionFromContext returns the conversation identity bound by
// WithStreamSession, or "" when the call site has none.
func StreamSessionFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(streamSessionKey{}).(string)
	return strings.TrimSpace(id)
}
