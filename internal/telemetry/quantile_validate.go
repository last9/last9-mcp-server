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
}

func (e *QuantileArgsError) Error() string {
	return fmt.Sprintf("$quantile at %s.$quantile must be exactly [level, field], with a numeric level in [0,1] first and a string field second", e.FunctionPath)
}

// ValidateQuantileFunction validates $quantile when present in an aggregation
// function. Other aggregation functions are left unchanged.
func ValidateQuantileFunction(function map[string]interface{}, functionPath string) *QuantileArgsError {
	rawArgs, ok := function["$quantile"]
	if !ok {
		return nil
	}

	args, ok := rawArgs.([]interface{})
	if !ok || len(args) != 2 {
		return &QuantileArgsError{FunctionPath: functionPath, Path: functionPath + ".$quantile"}
	}
	level, ok := args[0].(float64)
	if !ok || math.IsNaN(level) || level < 0 || level > 1 {
		return &QuantileArgsError{FunctionPath: functionPath, Path: functionPath + ".$quantile[0]"}
	}
	if _, ok := args[1].(string); !ok {
		return &QuantileArgsError{FunctionPath: functionPath, Path: functionPath + ".$quantile[1]"}
	}

	return nil
}
