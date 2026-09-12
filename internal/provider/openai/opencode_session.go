package openai

import (
	"context"
	"net/http"

	"reasonix/internal/provider"
)

// opencodeSessionHeader is the conversation-identity header the official
// OpenCode Go gateway requires on inference requests; it answers 400
// "Request is missing x-opencode-session" without it and uses the value to
// optimize prompt caching, mirroring the official client adapter.
const opencodeSessionHeader = "x-opencode-session"

// isOpenCodeGoChatBase reports whether baseURL is the exact official OpenCode
// Go chat base (https://opencode.ai/zen/go/v1). The decision is made once at
// construction from the configured base, matching the other vendor flags:
// look-alike hosts, custom ports, and proxy spellings never match.
func isOpenCodeGoChatBase(baseURL string) bool {
	_, ok := provider.OfficialOpenCodeGoRoute("openai", baseURL)
	return ok
}

// applySessionHeader replays the context-bound conversation identity on the
// official OpenCode Go route. It runs after applyCustomHeaders so a static
// configured value can never override the live session identity: a frozen
// header would merge distinct conversations into one cache lane.
func (c *client) applySessionHeader(h http.Header, ctx context.Context) {
	if c == nil || !c.openCodeGo {
		return
	}
	if session := provider.StreamSessionFromContext(ctx); session != "" {
		h.Set(opencodeSessionHeader, session)
	}
}
