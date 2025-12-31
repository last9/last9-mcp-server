package utils

import (
	"bytes"
	"io"
	"log"
	"net/http"
)

// DebugTransport wraps an http.RoundTripper and logs request URL and body
type DebugTransport struct {
	Transport http.RoundTripper
}

// RoundTrip implements http.RoundTripper interface
func (d *DebugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Log request URL
	log.Printf("[DEBUG] %s %s", req.Method, req.URL.String())

	// Log request body if present
	if req.Body != nil && req.ContentLength > 0 {
		bodyBytes, err := io.ReadAll(req.Body)
		if err == nil {
			req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			bodyStr := string(bodyBytes)
			if len(bodyStr) > 5000 {
				bodyStr = bodyStr[:5000] + "... [truncated]"
			}
			log.Printf("[DEBUG] Body: %s", bodyStr)
		}
	}

	return d.Transport.RoundTrip(req)
}

// WrapClientWithDebug wraps an http.Client with debug logging if debug is enabled
func WrapClientWithDebug(client *http.Client, debug bool) *http.Client {
	if !debug {
		return client
	}

	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	return &http.Client{
		Transport: &DebugTransport{Transport: transport},
		Timeout:   client.Timeout,
	}
}
