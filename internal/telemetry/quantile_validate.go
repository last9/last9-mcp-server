package telemetry

import (
	"fmt"
	"math"
)

// QuantileArgsError describes the domain-independent $quantile argument
// contract while preserving the caller-provided JSON path.
type QuantileArgsError struct {
	FunctionPath string
	Path         string
	Name         string
}

func (e *QuantileArgsError) Error() string {
	return fmt.Sprintf("%s at %s.%s must be exactly [level, field], with a finite numeric level in [0,1] first and a string field second", e.Name, e.FunctionPath, e.Name)
}

// ValidateQuantileFunction validates $quantile and $quantile_exact when present
// in an aggregation function. Other aggregation functions are left unchanged.
func ValidateQuantileFunction(function map[string]interface{}, functionPath string) *QuantileArgsError {
	for _, name := range []string{"$quantile", "$quantile_exact"} {
		rawArgs, present := function[name]
		if !present {
			continue
		}
		args, ok := rawArgs.([]interface{})
		if !ok || len(args) != 2 {
			return &QuantileArgsError{FunctionPath: functionPath, Path: functionPath + "." + name, Name: name}
		}
		level, ok := args[0].(float64)
		if !ok || math.IsNaN(level) || math.IsInf(level, 0) || level < 0 || level > 1 {
			return &QuantileArgsError{FunctionPath: functionPath, Path: functionPath + "." + name + "[0]", Name: name}
		}
		if _, ok := args[1].(string); !ok {
			return &QuantileArgsError{FunctionPath: functionPath, Path: functionPath + "." + name + "[1]", Name: name}
		}
	}
	return nil
}
