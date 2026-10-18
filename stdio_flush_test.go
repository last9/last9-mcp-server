package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const stdioSession = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"flush-test","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_current_time","arguments":{}}}
`

func stubJWT(aud string) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]string{"alg": "none"}) + "." + enc(map[string]any{
		"aud":               []string{aud},
		"organization_slug": "stub-org",
		"exp":               time.Now().Add(time.Hour).Unix(),
	}) + ".sig"
}

// runStdioFlushScenario starts the built binary against local stubs and returns
// its exit code and the number of trace and metric exports the receiver saw.
func runStdioFlushScenario(t *testing.T, sigterm bool) (exitCode int, traces, metrics int32) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs the binary")
	}
	if runtime.GOOS != "linux" {
		t.Skip("trusting the stub API's TLS certificate needs SSL_CERT_FILE, honored on linux only")
	}

	var tr, me atomic.Int32
	otlp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		switch r.URL.Path {
		case "/v1/traces":
			tr.Add(1)
		case "/v1/metrics":
			me.Add(1)
		}
	}))
	defer otlp.Close()

	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"access_token":%q}`, stubJWT("http://"+r.Host))
	}))
	defer oauth.Close()

	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"name":"stub","is_default":true,"url":"http://127.0.0.1:1","region":"r","properties":{"username":"u","password":"p","levitate_cluster_id":"c"}}]`)
	}))
	defer api.Close()

	dir := t.TempDir()
	certFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(dir, "last9-mcp")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cmd := exec.Command(bin)
	cmd.Dir = dir
	cmd.Env = []string{
		"LAST9_REFRESH_TOKEN=" + stubJWT("http://"+strings.TrimPrefix(oauth.URL, "http://")),
		"LAST9_API_HOST=" + strings.TrimPrefix(api.URL, "https://"),
		"SSL_CERT_FILE=" + certFile,
		"OTEL_SDK_DISABLED=false",
		"OTEL_EXPORTER_OTLP_ENDPOINT=" + otlp.URL,
		"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
	}
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	io.WriteString(stdin, stdioSession)
	if sigterm {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			if strings.Contains(sc.Text(), `"id":3`) {
				break
			}
		}
		cmd.Process.Signal(syscall.SIGTERM)
	} else {
		stdin.Close()
	}
	go io.Copy(io.Discard, stdout)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
	case <-time.After(12 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("process did not exit within 12s\n%s", stderr.String())
	}
	t.Logf("exit=%d trace_exports=%d metric_exports=%d", exitCode, tr.Load(), me.Load())
	if exitCode != 0 || tr.Load() == 0 {
		t.Logf("stderr:\n%s", stderr.String())
	}
	return exitCode, tr.Load(), me.Load()
}

func TestStdioFlushesTelemetryOnStdinClose(t *testing.T) {
	code, traces, _ := runStdioFlushScenario(t, false)
	if code != 0 || traces == 0 {
		t.Fatalf("exit=%d traces=%d, want exit 0 and at least one trace export", code, traces)
	}
}

func TestStdioFlushesTelemetryOnSIGTERM(t *testing.T) {
	code, traces, _ := runStdioFlushScenario(t, true)
	if code != 0 || traces == 0 {
		t.Fatalf("exit=%d traces=%d, want exit 0 and at least one trace export", code, traces)
	}
}
