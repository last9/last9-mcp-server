package traces

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetTracesHandlerKeepsTraceFieldsVerbatim(t *testing.T) {
	const body = `{"status":"success","data":{"resultType":"traces","result":[` +
		`{"TraceId":"abc","Timestamp":1761550000123456789,"Duration":12345678901234567}]}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery:  []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}},
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{
		`"Timestamp": 1761550000123456789`,
		`"Duration": 12345678901234567`,
		`"status": "success"`,
		`"resultType": "traces"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("result lacks %s:\n%s", want, text)
		}
	}
}

// Generic maps cost several times the wire size per trace; a chunk's decoded
// traces must stay close to its body size.
func TestExecuteTraceJSONQueryRetainsAboutTheBodySize(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"data":{"result":[`)
	for i := range 5000 {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"TraceId":"%032x","SpanId":"%016x","ServiceName":"checkout","SpanName":"GET /cart","Duration":%d,"Timestamp":%d,"attributes":{"http.method":"GET","http.status_code":200}}`,
			i, i, 1000+i, 1761550000000000000+i)
	}
	sb.WriteString(`]}}`)
	body := sb.String()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	cfg := testChunkTracesConfig(server.URL)

	before := liveHeapBytes()
	value, err := executeTraceJSONQuery(context.Background(), server.Client(), cfg, []interface{}{}, 0, 1000, 5000)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	retained := int64(liveHeapBytes()) - int64(before)
	runtime.KeepAlive(value)

	if limit := int64(2 * len(body)); retained > limit {
		t.Errorf("decoded chunk retains %d bytes for a %d-byte body, want <= %d", retained, len(body), limit)
	}
}

func liveHeapBytes() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}
