// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package metriccatalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

type Definition struct {
	Type   string `json:"type"`
	Unit   string `json:"unit"`
	Help   string `json:"help"`
	Source string `json:"source"`
}

type ReceiverMetric struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Monotonic bool   `json:"monotonic"`
	Unit      string `json:"unit"`
	Help      string `json:"help"`
}

type ReceiverSource struct {
	Path    string           `json:"path"`
	Version string           `json:"version"`
	SHA256  string           `json:"sha256"`
	Metrics []ReceiverMetric `json:"metrics"`
}

//go:embed receivers_generated.json
var receiverFiles embed.FS

var receiverLookup = loadReceivers()

func loadReceivers() map[string]Definition {
	raw, err := receiverFiles.ReadFile("receivers_generated.json")
	if err != nil {
		panic(err)
	}
	var out map[string]Definition
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

func Receiver(name string) (Definition, bool) {
	d, ok := receiverLookup[name]
	return d, ok
}

func GenerateReceivers(sources []ReceiverSource) ([]byte, error) {
	lookup := map[string]Definition{}
	ambiguous := map[string]bool{}
	for _, source := range sources {
		for _, metric := range source.Metrics {
			kind := "gauge"
			switch metric.Kind {
			case "sum":
				if metric.Monotonic {
					kind = "counter"
				}
			case "gauge":
			case "histogram", "exponential_histogram":
				kind = "histogram"
			default:
				return nil, fmt.Errorf("unknown receiver metric kind %q", metric.Kind)
			}
			d := Definition{Type: kind, Unit: metric.Unit, Help: metric.Help, Source: source.Path + "@" + source.Version}
			for _, name := range []string{strings.ReplaceAll(metric.Name, ".", "_"), translatedName(metric)} {
				if old, ok := lookup[name]; ok && (old.Type != d.Type || old.Unit != d.Unit || old.Help != d.Help) {
					ambiguous[name] = true
				}
				if !ambiguous[name] {
					lookup[name] = d
				} else {
					delete(lookup, name)
				}
			}
		}
	}
	raw, err := json.MarshalIndent(lookup, "", "  ")
	return append(raw, '\n'), err
}

// Match the pinned OTel translator with and without metric suffixes; units are not rescaled.
func translatedName(m ReceiverMetric) string {
	tokens := strings.FieldsFunc(m.Name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	units := strings.SplitN(m.Unit, "/", 2)
	appendUnit := func(unit string, mapping map[string]string, per bool) {
		unit = strings.TrimSpace(unit)
		if strings.ContainsAny(unit, "{}") {
			return
		}
		if translated, ok := mapping[unit]; ok {
			unit = translated
		}
		unit = strings.Join(strings.FieldsFunc(unit, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), "_")
		if unit != "" && !slices.Contains(tokens, unit) {
			if per {
				tokens = append(tokens, "per")
			}
			tokens = append(tokens, unit)
		}
	}
	appendUnit(units[0], prometheusUnits, false)
	if len(units) == 2 {
		appendUnit(units[1], map[string]string{"s": "second", "m": "minute", "h": "hour", "d": "day", "w": "week", "mo": "month", "y": "year"}, true)
	}
	if m.Kind == "sum" && m.Monotonic {
		tokens = append(slices.DeleteFunc(tokens, func(s string) bool { return s == "total" }), "total")
	}
	if m.Kind == "gauge" && m.Unit == "1" {
		tokens = append(slices.DeleteFunc(tokens, func(s string) bool { return s == "ratio" }), "ratio")
	}
	name := strings.Join(tokens, "_")
	if len(name) > 0 && name[0] >= '0' && name[0] <= '9' {
		name = "_" + name
	}
	return name
}

var prometheusUnits = map[string]string{
	"d": "days", "h": "hours", "min": "minutes", "s": "seconds", "ms": "milliseconds", "us": "microseconds", "ns": "nanoseconds",
	"By": "bytes", "KiBy": "kibibytes", "MiBy": "mebibytes", "GiBy": "gibibytes", "TiBy": "tibibytes", "KBy": "kilobytes", "MBy": "megabytes", "GBy": "gigabytes", "TBy": "terabytes",
	"m": "meters", "V": "volts", "A": "amperes", "J": "joules", "W": "watts", "g": "grams", "Cel": "celsius", "Hz": "hertz", "1": "", "%": "percent",
}
