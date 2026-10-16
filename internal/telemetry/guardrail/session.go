package guardrail

import (
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sessionTTL forgets a session idle this long. HTTP mode is stateless, so no
// close event ever arrives to free its entry.
const sessionTTL = time.Hour

const (
	sessionIDHeader = "Mcp-Session-Id"
	stdioSessionKey = "stdio"
)

// processSessions is shared by every guarded tool in the process, so a
// session's budget spans all of them.
var processSessions = newSessions(time.Now)

type sessions struct {
	mu    sync.Mutex
	now   func() time.Time
	usage map[string]*sessionUsage
}

type sessionUsage struct {
	bytes    int64
	lastSeen time.Time
}

func newSessions(now func() time.Time) *sessions {
	return &sessions{now: now, usage: map[string]*sessionUsage{}}
}

// add records bytes against the session and returns its running total.
func (s *sessions) add(key string, bytes int64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	u, ok := s.usage[key]
	if !ok || now.Sub(u.lastSeen) > sessionTTL {
		s.evictIdle(now)
		u = &sessionUsage{}
		s.usage[key] = u
	}
	u.bytes += bytes
	u.lastSeen = now
	return u.bytes
}

func (s *sessions) evictIdle(now time.Time) {
	for key, u := range s.usage {
		if now.Sub(u.lastSeen) > sessionTTL {
			delete(s.usage, key)
		}
	}
}

// sessionKey names the MCP session a call belongs to. Stdio serves one
// session per process; an HTTP call without Mcp-Session-Id has none to track.
func sessionKey(req *mcp.CallToolRequest) (string, bool) {
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return stdioSessionKey, true
	}
	id := req.Extra.Header.Get(sessionIDHeader)
	return id, id != ""
}
