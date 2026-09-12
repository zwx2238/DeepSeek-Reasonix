package provider

import (
	"context"
	"testing"
)

func TestStreamSessionRoundTrip(t *testing.T) {
	ctx := WithStreamSession(context.Background(), " sess-abc ")
	if got := StreamSessionFromContext(ctx); got != "sess-abc" {
		t.Fatalf("StreamSessionFromContext = %q, want sess-abc", got)
	}
}

func TestStreamSessionEmptyBindsNothing(t *testing.T) {
	ctx := WithStreamSession(context.Background(), "  ")
	if ctx.Value(streamSessionKey{}) != nil {
		t.Fatal("blank session id must not bind a value")
	}
	if got := StreamSessionFromContext(ctx); got != "" {
		t.Fatalf("StreamSessionFromContext = %q, want empty", got)
	}
}

func TestStreamSessionFromNilContext(t *testing.T) {
	if got := StreamSessionFromContext(nil); got != "" {
		t.Fatalf("StreamSessionFromContext(nil) = %q, want empty", got)
	}
}

func TestStreamSessionRebindTakesPrecedence(t *testing.T) {
	ctx := WithStreamSession(context.Background(), "first")
	ctx = WithStreamSession(ctx, "second")
	if got := StreamSessionFromContext(ctx); got != "second" {
		t.Fatalf("StreamSessionFromContext = %q, want second", got)
	}
}

func TestStreamSessionInheritedByDerivedContext(t *testing.T) {
	ctx := WithStreamSession(context.Background(), "sess-abc")
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	if got := StreamSessionFromContext(child); got != "sess-abc" {
		t.Fatalf("derived context lost the session binding: %q", got)
	}
}
