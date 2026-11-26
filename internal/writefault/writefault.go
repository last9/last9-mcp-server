// Package writefault serves the upstream failures after which a write may or may not
// have been applied. Only tests import it.
package writefault

import (
	"io"
	"net/http"
)

type Fault string

const (
	// Hang reads the request and never answers; the client must give up on its own timeout.
	Hang Fault = "hang"
	// Close reads the request, then closes the connection without sending a status.
	Close Fault = "close"
	// Truncated sends 200 and part of the declared body, then closes the connection.
	Truncated Fault = "truncated"
)

var All = []Fault{Hang, Close, Truncated}

// Serve answers r with f.
func Serve(w http.ResponseWriter, r *http.Request, f Fault) {
	_, _ = io.Copy(io.Discard, r.Body)
	switch f {
	case Hang:
		<-r.Context().Done()
	case Close:
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			panic(err)
		}
		_ = conn.Close()
	case Truncated:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"`))
	default:
		panic("unknown fault " + string(f))
	}
}
