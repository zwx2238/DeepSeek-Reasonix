package control

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type observedStreamSession struct {
	input   string
	session string
}

type streamSessionRecordingRunner struct {
	observed chan<- observedStreamSession
}

func (r streamSessionRecordingRunner) Run(ctx context.Context, input string) error {
	r.observed <- observedStreamSession{input: input, session: provider.StreamSessionFromContext(ctx)}
	return nil
}

func receiveObservedStreamSession(t *testing.T, observed <-chan observedStreamSession) observedStreamSession {
	t.Helper()
	select {
	case got := <-observed:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for submitted turn")
		return observedStreamSession{}
	}
}

// TestRunTurnBindsSessionAffinity：同步 turn（ACP session/prompt 路径）的
// provider 请求 ctx 携带会话分支 id——x-opencode-session 的本地身份语义。
func TestRunTurnBindsSessionAffinity(t *testing.T) {
	observed := make(chan observedStreamSession, 1)
	c := New(Options{
		Runner:      streamSessionRecordingRunner{observed: observed},
		SessionPath: filepath.Join(t.TempDir(), "sess-abc.jsonl"),
	})
	if err := c.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := receiveObservedStreamSession(t, observed)
	if got.session != "sess-abc" {
		t.Fatalf("turn ctx session = %q, want sess-abc", got.session)
	}
}

// TestSubmitHTTPBindsSessionAffinity：异步 turn（TUI/HTTP 前端路径）同样绑定。
func TestSubmitHTTPBindsSessionAffinity(t *testing.T) {
	observed := make(chan observedStreamSession, 1)
	c := New(Options{
		Runner:      streamSessionRecordingRunner{observed: observed},
		SessionPath: filepath.Join(t.TempDir(), "sess-http.jsonl"),
	})
	c.SubmitHTTP("hello")
	got := receiveObservedStreamSession(t, observed)
	if got.session != "sess-http" {
		t.Fatalf("turn ctx session = %q, want sess-http", got.session)
	}
}

// TestDistinctSessionsCarryDistinctAffinity：不同会话路径绑定不同身份；
// 会话隔离是 x-opencode-session 缓存亲和的前提。
func TestDistinctSessionsCarryDistinctAffinity(t *testing.T) {
	firstObserved := make(chan observedStreamSession, 1)
	first := New(Options{
		Runner:      streamSessionRecordingRunner{observed: firstObserved},
		SessionPath: filepath.Join(t.TempDir(), "sess-one.jsonl"),
	})
	secondObserved := make(chan observedStreamSession, 1)
	second := New(Options{
		Runner:      streamSessionRecordingRunner{observed: secondObserved},
		SessionPath: filepath.Join(t.TempDir(), "sess-two.jsonl"),
	})
	if err := first.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("first RunTurn: %v", err)
	}
	if err := second.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("second RunTurn: %v", err)
	}
	a := receiveObservedStreamSession(t, firstObserved)
	b := receiveObservedStreamSession(t, secondObserved)
	if a.session == "" || b.session == "" || a.session == b.session {
		t.Fatalf("distinct sessions must bind distinct identities, got %q and %q", a.session, b.session)
	}
}

// TestSessionAffinityFollowsSessionPathSwitch：会话切换后绑定跟随新会话身份。
func TestSessionAffinityFollowsSessionPathSwitch(t *testing.T) {
	observed := make(chan observedStreamSession, 2)
	c := New(Options{
		Runner:      streamSessionRecordingRunner{observed: observed},
		SessionPath: filepath.Join(t.TempDir(), "sess-old.jsonl"),
	})
	if err := c.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("RunTurn before switch: %v", err)
	}
	c.SetSessionPath(filepath.Join(t.TempDir(), "sess-new.jsonl"))
	if err := c.RunTurn(context.Background(), "hello again"); err != nil {
		t.Fatalf("RunTurn after switch: %v", err)
	}
	first := receiveObservedStreamSession(t, observed)
	second := receiveObservedStreamSession(t, observed)
	if first.session != "sess-old" || second.session != "sess-new" {
		t.Fatalf("session affinity did not follow the switch: %q then %q", first.session, second.session)
	}
}

// TestSessionAffinityAbsentWithoutSessionPath：无持久化会话不绑定（不虚构身份）。
func TestSessionAffinityAbsentWithoutSessionPath(t *testing.T) {
	observed := make(chan observedStreamSession, 1)
	c := New(Options{
		Runner: streamSessionRecordingRunner{observed: observed},
		Sink:   event.Discard,
	})
	if err := c.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := receiveObservedStreamSession(t, observed)
	if got.session != "" {
		t.Fatalf("turn ctx session = %q, want empty for path-less session", got.session)
	}
}

// TestBranchIDIsSessionAffinitySource：hooks 与 provider 会话头使用同一身份来源。
func TestBranchIDIsSessionAffinitySource(t *testing.T) {
	if got := agent.BranchID(filepath.Join("dir", "sess-abc.jsonl")); got != "sess-abc" {
		t.Fatalf("BranchID = %q, want sess-abc", got)
	}
}
