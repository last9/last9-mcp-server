package prom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestDiscoverLabelNames(t *testing.T) {
	for _, tt := range []struct {
		body string
		bad  bool
	}{
		{`{"labels":["service_name","env"]}`, false},
		{`{"labels":[]}`, false},
		{`{"labels":null}`, true},
		{`{"labels":["env"],"partial":true}`, true},
		{`{"labels":["env"],"warnings":["truncated"]}`, true},
		{`{"labels":["env"],"status":"error"}`, true},
		{`{"labels":["env"]} {}`, true},
		{`{}`, true},
		{`null`, true},
		{``, true},
	} {
		t.Run(tt.body, func(t *testing.T) {
			input := GetAPMLabelsInput{Datasource: testDatasource, Metric: `requests{env="prod"}`, Timestamp: 1700000600, Window: 600}
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				var got GetAPMLabelsInput
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if got != input || r.URL.Path != "/apm/labels" {
					t.Errorf("request=%s %+v", r.URL.Path, got)
				}
				_, _ = io.WriteString(w, tt.body)
			})
			names, err := DiscoverLabelNames(context.Background(), client, input)
			if (err != nil) != tt.bad {
				t.Fatalf("names=%v err=%v", names, err)
			}
		})
	}
}
