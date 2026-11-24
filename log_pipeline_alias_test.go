package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetLogsPipelineAlias(t *testing.T) {
	bodies := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		bodies <- string(body)
		io.WriteString(w, `{"status":"success","data":{"resultType":"streams","result":[]}}`)
	}))
	defer srv.Close()
	cfg := writeToolTestConfig(srv.URL)
	cfg.UseLogSearchAPI = false
	session := connectTestSession(t, cfg, srv.Client())
	const stages = `[{"type":"filter","query":{"$and":[{"$eq":["ServiceName","example"]}]}}]`
	const reordered = `[{"query":{"$and":[{"$eq":["ServiceName","example"]}]},"type":"filter"}]`
	var canonical string
	for _, tc := range []struct{ args, problem string }{
		{`{"logjson_query":` + stages + `}`, ""},
		{`{"pipeline":` + stages + `}`, ""},
		{`{"pipeline":` + reordered + `,"logjson_query":` + stages + `}`, ""},
		{`{"pipeline":[],"logjson_query":` + stages + `}`, "conflicting"},
		{`{"pipeline":"oops"}`, `argument "pipeline" must be an array`},
		{`{"pipeline":null}`, `argument "pipeline" must be an array`},
		{`{"pipeline":{"type":"filter"}}`, `argument "pipeline" must be an array`},
		{`{"pipeline":` + stages + `,"unknown":true}`, "unknown"},
	} {
		res := callRegisteredTool(t, session, &mcp.CallToolParams{Name: "get_logs", Arguments: json.RawMessage(tc.args)})
		if tc.problem != "" {
			if !res.IsError || !strings.Contains(toolResultText(res), tc.problem) {
				t.Errorf("%s: %s", tc.args, toolResultText(res))
			}
			if len(bodies) != 0 {
				t.Fatal("invalid call reached upstream")
			}
			continue
		}
		if res.IsError {
			t.Fatalf("%s: %s", tc.args, toolResultText(res))
		}
		select {
		case body := <-bodies:
			if canonical == "" {
				canonical = body
			} else if canonical != body {
				t.Fatalf("different upstream body: %s", body)
			}
		default:
			t.Fatal("missing upstream call")
		}
	}
}
