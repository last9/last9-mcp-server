package logs

import (
	"reflect"
	"strings"
	"testing"
)

// jsonParam reports whether a struct exposes a JSON property `name`, and
// whether it is optional (has the `omitempty` option).
func jsonParam(rt reflect.Type, name string) (present, optional bool) {
	for i := 0; i < rt.NumField(); i++ {
		parts := strings.Split(rt.Field(i).Tag.Get("json"), ",")
		if parts[0] == name {
			present = true
			for _, p := range parts[1:] {
				if p == "omitempty" {
					optional = true
				}
			}
		}
	}
	return
}

func TestGetServiceLogsArgs_UsesRequiredServiceName(t *testing.T) {
	rt := reflect.TypeOf(GetServiceLogsArgs{})

	present, optional := jsonParam(rt, "service_name")
	if !present {
		t.Fatal("GetServiceLogsArgs must expose canonical param \"service_name\"")
	}
	if optional {
		t.Fatal("service_name must be required (no omitempty)")
	}
	if p, _ := jsonParam(rt, "service"); p {
		t.Fatal("legacy param \"service\" must be removed")
	}
}
