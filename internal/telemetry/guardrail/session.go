package guardrail

import (
	"container/list"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sessionTTL forgets a session idle this long. HTTP mode is stateless, so no
// close event ever arrives to free its entry.
const sessionTTL = time.Hour

// maxSessions bounds the map: over HTTP the session id is client-chosen, so a
// caller minting ids would otherwise grow it until the TTL. Past the cap the
// least recently seen session is forgotten; the session budget only warns, so
// that costs a warning, never a blocked query.
const maxSessions = 10_000

const (
	sessionIDHeader = "Mcp-Session-Id"
	stdioSessionKey = "stdio"
)

// processSessions is shared by every guarded tool in the process, so a
// session's budget spans all of them.
var processSessions = newSessions(time.Now)

// sessions keeps usage in a list ordered by recency, most recent first, so
// the idle and the cap evictions both pop from the back.
type sessions struct {
	mu     sync.Mutex
	now    func() time.Time
	usage  map[string]*list.Element
	recent *list.List // of *sessionUsage
}

type sessionUsage struct {
	key      string
	bytes    int64
	lastSeen time.Time
}

func newSessions(now func() time.Time) *sessions {
	return &sessions{now: now, usage: map[string]*list.Element{}, recent: list.New()}
}

// add records bytes against the session and returns its running total.
func (s *sessions) add(key string, bytes int64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.evictIdle(now)
	e, ok := s.usage[key]
	if ok {
		s.recent.MoveToFront(e)
	} else {
		e = s.recent.PushFront(&sessionUsage{key: key})
		s.usage[key] = e
		for s.recent.Len() > maxSessions {
			s.remove(s.recent.Back())
		}
	}
	u := e.Value.(*sessionUsage)
	u.bytes += bytes
	u.lastSeen = now
	return u.bytes
}

func (s *sessions) evictIdle(now time.Time) {
	for e := s.recent.Back(); e != nil && now.Sub(e.Value.(*sessionUsage).lastSeen) > sessionTTL; e = s.recent.Back() {
		s.remove(e)
	}
}

func (s *sessions) remove(e *list.Element) {
	delete(s.usage, s.recent.Remove(e).(*sessionUsage).key)
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
