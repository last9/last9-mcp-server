package apitest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func Server(t *testing.T, handler http.Handler, inMemory bool) (string, *http.Client) {
	t.Helper()
	if inMemory {
		return "https://api.example", &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			if err := r.Context().Err(); err != nil {
				return nil, err
			}
			return recorder.Result(), nil
		})}
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL, srv.Client()
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
