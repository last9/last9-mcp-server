package alerting

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const channelSecret = "SECRET-SENTINEL-9f3"

type recordedCall struct{ method, uri, body string }

type channelServer struct {
	*httptest.Server
	mu    sync.Mutex
	calls []recordedCall
}

// respond maps a request to a status and body; every request is recorded.
func newChannelServer(t *testing.T, respond func(r *http.Request) (int, string)) *channelServer {
	s := &channelServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.calls = append(s.calls, recordedCall{r.Method, r.URL.RequestURI(), string(b)})
		s.mu.Unlock()
		status, body := respond(r)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *channelServer) count(method string) int {
	n := 0
	for _, c := range s.calls {
		if c.method == method {
			n++
		}
	}
	return n
}

func assertNoSecret(t *testing.T, text string, err error) {
	t.Helper()
	if strings.Contains(text, channelSecret) || (err != nil && strings.Contains(err.Error(), channelSecret)) {
		t.Fatalf("secret leaked: text=%q err=%v", text, err)
	}
}
