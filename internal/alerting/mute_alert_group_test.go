package alerting

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMuteAlertGroupHandler(t *testing.T) {
	for _, tt := range []struct {
		name  string
		until int64
		body  string
	}{
		{"mute until", 1900000000, `{"until":1900000000}`},
		{"unmute", 0, `{"until":0}`},
		{"indefinite", -1, `{"until":-1}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.EscapedPath() != "/entities/entity-1/alert-rules/snooze" {
					t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
				}
				if got, _ := io.ReadAll(r.Body); string(got) != tt.body {
					t.Errorf("body = %s", got)
				}
				_, _ = w.Write([]byte(`{"ident":"entity-1"}`))
			}))
			defer server.Close()
			result, _, err := NewMuteAlertGroupHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
				MuteAlertGroupArgs{EntityID: "entity-1", Until: tt.until})
			if err != nil || result.IsError || resultText(result) != `{"ident":"entity-1"}` {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestMuteAlertGroupRequiresEntityID(t *testing.T) {
	result, _, err := NewMuteAlertGroupHandler(http.DefaultClient, testAlertMutationConfig("http://unused"))(context.Background(), nil, MuteAlertGroupArgs{Until: 1})
	if err != nil || !result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestMuteAlertGroupRejectsOmittedOrNullUntil(t *testing.T) {
	for name, raw := range map[string]string{
		"omitted": `{"entity_id":"entity-1"}`,
		"null":    `{"entity_id":"entity-1","until":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = w.Write([]byte(`{"ident":"entity-1"}`))
			}))
			defer server.Close()
			var args MuteAlertGroupArgs
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				t.Fatal(err)
			}
			result, _, err := NewMuteAlertGroupHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, args)
			if err != nil || !result.IsError || !strings.Contains(resultText(result), "until is required") {
				t.Fatalf("result=%s err=%v", resultText(result), err)
			}
			if calls != 0 {
				t.Fatalf("snooze called %d times; an omitted until must not unmute the group", calls)
			}
		})
	}
}
