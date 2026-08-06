package pulse

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

const contractFixtureSHA256 = "25a97da2661d9c1700b194e84dbc12d22379ef7b8a060d7c030d1d7a8de8cf54"

//go:embed testdata/alert_hygiene_pulse_contract_v1.json
var contractFixtureJSON []byte

type contractFixture struct {
	FixtureVersion    string          `json:"fixture_version"`
	FixturePurpose    string          `json:"fixture_purpose"`
	Subscription      json.RawMessage `json:"subscription"`
	ReportRead        json.RawMessage `json:"report_read"`
	FindingsPage      json.RawMessage `json:"findings_page"`
	FindingDetail     json.RawMessage `json:"finding_detail"`
	EvidencePage      json.RawMessage `json:"evidence_page"`
	DispositionWrite  json.RawMessage `json:"disposition_write"`
	DispositionRecord json.RawMessage `json:"disposition_record"`
}

func TestCrossSurfaceFixtureReadsCanonicalRecords(t *testing.T) {
	fixture := loadContractFixture(t)
	client := contractClient(t, fixture, nil)
	config := pulseTestConfig()
	subscription, _, err := NewGetSubscriptionHandler(client, config)(
		context.Background(), nil, GetSubscriptionArgs{SubscriptionID: "subscription-fixture-v1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	report, _, err := NewGetReportHandler(client, config)(
		context.Background(), nil, GetReportArgs{RunID: "run-fixture-v1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	findings, _, err := NewListFindingsHandler(client, config)(
		context.Background(), nil, RunPageArgs{RunID: "run-fixture-v1", Limit: 25},
	)
	if err != nil {
		t.Fatal(err)
	}
	detail, _, err := NewGetFindingHandler(client, config)(context.Background(), nil,
		GetFindingArgs{RunID: "run-fixture-v1", OccurrenceID: "occurrence-fixture-v1"})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, resultText(t, subscription), fixture.Subscription)
	assertJSONEqual(t, resultText(t, report), fixture.ReportRead)
	assertJSONEqual(t, resultText(t, findings), fixture.FindingsPage)
	assertJSONEqual(t, resultText(t, detail), fixture.FindingDetail)
	if string(fixture.EvidencePage) == "" || jsonContainsKey(fixture.EvidencePage, "payload") {
		t.Fatal("safe evidence fixture is empty or exposes raw payload")
	}
}

func TestCrossSurfaceFixtureWritesCanonicalDisposition(t *testing.T) {
	fixture := loadContractFixture(t)
	var captured map[string]any
	client := contractClient(t, fixture, &captured)
	args := WriteDispositionArgs{FindingID: "finding-fixture-v1", Confirmed: true}
	if err := json.Unmarshal(fixture.DispositionWrite, &args); err != nil {
		t.Fatal(err)
	}
	result, _, err := NewWriteDispositionHandler(client, pulseTestConfig())(
		context.Background(), nil, args,
	)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal(fixture.DispositionWrite, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(captured, want) {
		t.Fatalf("disposition body = %#v, want %#v", captured, want)
	}
	assertJSONEqual(t, resultText(t, result), fixture.DispositionRecord)
}

func loadContractFixture(t *testing.T) contractFixture {
	t.Helper()
	digest := fmt.Sprintf("%x", sha256.Sum256(contractFixtureJSON))
	if digest != contractFixtureSHA256 {
		t.Fatalf("contract fixture checksum = %s, want %s", digest, contractFixtureSHA256)
	}
	var fixture contractFixture
	if err := json.Unmarshal(contractFixtureJSON, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.FixtureVersion != "alert-hygiene-pulse-cross-surface-v1" {
		t.Fatalf("fixture version = %q", fixture.FixtureVersion)
	}
	return fixture
}

func contractClient(t *testing.T, fixture contractFixture, captured *map[string]any) *http.Client {
	t.Helper()
	return pulseTestClient(func(request *http.Request) (int, string) {
		switch request.URL.Path {
		case pulseBasePath + "/subscriptions/subscription-fixture-v1":
			return http.StatusOK, string(fixture.Subscription)
		case pulseBasePath + "/runs/run-fixture-v1/report":
			return http.StatusOK, string(fixture.ReportRead)
		case pulseBasePath + "/runs/run-fixture-v1/findings":
			if request.URL.Query().Get("limit") != "25" {
				t.Fatalf("findings query = %s", request.URL.RawQuery)
			}
			return http.StatusOK, string(fixture.FindingsPage)
		case pulseBasePath + "/runs/run-fixture-v1/findings/occurrence-fixture-v1":
			return http.StatusOK, string(fixture.FindingDetail)
		case pulseBasePath + "/findings/finding-fixture-v1/disposition":
			if captured != nil {
				if err := json.NewDecoder(request.Body).Decode(captured); err != nil {
					t.Fatal(err)
				}
			}
			return http.StatusOK, string(fixture.DispositionRecord)
		default:
			return http.StatusNotFound, `{"error":"unexpected fixture path"}`
		}
	})
}

func assertJSONEqual(t *testing.T, got string, want json.RawMessage) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON mismatch\ngot:  %s\nwant: %s", got, string(want))
	}
}

func jsonContainsKey(raw json.RawMessage, key string) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	return containsJSONKey(value, key)
}

func containsJSONKey(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if _, exists := typed[key]; exists {
			return true
		}
		for _, child := range typed {
			if containsJSONKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsJSONKey(child, key) {
				return true
			}
		}
	}
	return false
}
