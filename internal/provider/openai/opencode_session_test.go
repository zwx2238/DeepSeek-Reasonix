package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/provider"
)

// openCodeGoTestServer mirrors the official OpenCode Go gateway contract:
// an inference request without x-opencode-session is rejected with the
// gateway's own 400 body, a present header is recorded per request.
func openCodeGoTestServer(t *testing.T, mu *sync.Mutex, captured *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := r.Header.Get("x-opencode-session")
		mu.Lock()
		*captured = append(*captured, session)
		mu.Unlock()
		if session == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"type":"api_error","message":"Request is missing x-opencode-session"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
}

func newOpenCodeGoClient(t *testing.T, srvURL string, extra map[string]any) *client {
	t.Helper()
	p, err := New(provider.Config{
		Name:    "opencode-go",
		BaseURL: "https://opencode.ai/zen/go/v1",
		Model:   "deepseek-v4-flash",
		APIKey:  "test-key",
		Extra:   extra,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if !c.openCodeGo {
		t.Fatal("official OpenCode Go base did not enable the session header contract")
	}
	c.chatURL = srvURL
	return c
}

func drainStream(t *testing.T, c *client, ctx context.Context) error {
	t.Helper()
	ch, err := c.Stream(ctx, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		return err
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			return chunk.Err
		}
	}
	return nil
}

// TestOpenCodeGoStreamSendsSessionHeader：官方路由的普通流式请求必须携带
// x-opencode-session；缺失时网关以 400 拒绝（生产故障复现），携带后流式完成。
func TestOpenCodeGoStreamSendsSessionHeader(t *testing.T) {
	var mu sync.Mutex
	var captured []string
	srv := openCodeGoTestServer(t, &mu, &captured)
	defer srv.Close()

	c := newOpenCodeGoClient(t, srv.URL, nil)
	if err := drainStream(t, c, context.Background()); err == nil {
		t.Fatal("header-less stream must be rejected by the gateway contract")
	} else if !strings.Contains(err.Error(), "Request is missing x-opencode-session") {
		t.Fatalf("header-less stream error = %v, want the gateway's missing-session rejection", err)
	}
	if err := drainStream(t, c, provider.WithStreamSession(context.Background(), "sess-abc")); err != nil {
		t.Fatalf("Stream with bound session: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 2 || captured[0] != "" || captured[1] != "sess-abc" {
		t.Fatalf("captured x-opencode-session = %v, want [ sess-abc]", captured)
	}
}

// TestOpenCodeGoSessionHeaderIsolatesConversations：同一 provider 实例上，
// 不同会话绑定产生不同的 header 值，同一会话跨请求保持稳定。
func TestOpenCodeGoSessionHeaderIsolatesConversations(t *testing.T) {
	var mu sync.Mutex
	var captured []string
	srv := openCodeGoTestServer(t, &mu, &captured)
	defer srv.Close()

	c := newOpenCodeGoClient(t, srv.URL, nil)
	first := provider.WithStreamSession(context.Background(), "sess-one")
	second := provider.WithStreamSession(context.Background(), "sess-two")
	for _, ctx := range []context.Context{first, second, first} {
		if err := drainStream(t, c, ctx); err != nil {
			t.Fatalf("Stream: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"sess-one", "sess-two", "sess-one"}
	if len(captured) != len(want) {
		t.Fatalf("captured %v, want %v", captured, want)
	}
	for i := range want {
		if captured[i] != want[i] {
			t.Fatalf("captured %v, want %v", captured, want)
		}
	}
}

// TestOpenCodeGoSessionHeaderMissingWithoutBinding：ctx 未绑定会话时不出头。
// 会话身份是调用方契约：适配器不虚构值，否则缓存隔离被静默破坏。
func TestOpenCodeGoSessionHeaderMissingWithoutBinding(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-opencode-session")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	c := newOpenCodeGoClient(t, srv.URL, nil)
	if err := drainStream(t, c, context.Background()); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got != "" {
		t.Fatalf("x-opencode-session = %q without a bound session, want omitted", got)
	}
}

// TestOpenCodeGoSessionHeaderOverridesStaticConfig：静态配置的 header 不能
// 覆盖会话身份——固定值会把不同会话并入同一缓存车道。
func TestOpenCodeGoSessionHeaderOverridesStaticConfig(t *testing.T) {
	var mu sync.Mutex
	var captured []string
	srv := openCodeGoTestServer(t, &mu, &captured)
	defer srv.Close()

	c := newOpenCodeGoClient(t, srv.URL, map[string]any{
		"headers": map[string]string{"x-opencode-session": "static-config"},
	})
	if err := drainStream(t, c, provider.WithStreamSession(context.Background(), "sess-live")); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 || captured[0] != "sess-live" {
		t.Fatalf("captured x-opencode-session = %v, want live session [sess-live]", captured)
	}
}

// TestCustomEndpointOmitsSessionHeader：非官方路由即使绑定会话也不出头，
// x-opencode-session 是 OpenCode Go 网关的协议要求，不是通用 OpenAI 语义。
func TestCustomEndpointOmitsSessionHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-opencode-session")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{
		Name:    "custom",
		BaseURL: srv.URL,
		Model:   "model-a",
		APIKey:  "k",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if c.openCodeGo {
		t.Fatal("custom endpoint must not enable the OpenCode Go session contract")
	}
	if err := drainStream(t, c, provider.WithStreamSession(context.Background(), "sess-abc")); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got != "" {
		t.Fatalf("x-opencode-session = %q on custom endpoint, want omitted", got)
	}
}

func TestIsOpenCodeGoChatBase(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		{"https://opencode.ai/zen/go/v1", true},
		{"https://opencode.ai/zen/go/v1/", true},
		{"https://opencode.ai/zen/go", false}, // anthropic 路由，非本 chat 契约
		{"https://opencode.ai/zen/go/v1beta", false},
		{"https://opencode.ai/zen/v1", false},
		{"http://opencode.ai/zen/go/v1", false},
		{"https://opencode.ai:8443/zen/go/v1", false},
		{"https://user@opencode.ai/zen/go/v1", false},
		{"https://opencode.ai.evil.test/zen/go/v1", false},
		{"https://mirror.example/zen/go/v1", false},
		{"", false},
	} {
		if got := isOpenCodeGoChatBase(tc.baseURL); got != tc.want {
			t.Errorf("isOpenCodeGoChatBase(%q) = %v, want %v", tc.baseURL, got, tc.want)
		}
	}
}
