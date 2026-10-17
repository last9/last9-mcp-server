package traces

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

type exceptionIdentity struct {
	Fingerprint string
	ID          string
}

// identifyException returns the fingerprint and row id Discover Exceptions gives the same row.
// The env joins the id only in All-envs mode, where one exception appears once per environment.
func identifyException(e exceptionAggregate, isAllEnvs bool) exceptionIdentity {
	fingerprint := exceptionFingerprint(e.ExceptionType + ":" + e.ServiceName + ":" + e.SpanName)
	parts := []string{fingerprint, e.ServiceName, e.SpanName}
	if isAllEnvs {
		parts = append(parts, e.DeploymentEnvironment)
	}
	return exceptionIdentity{Fingerprint: fingerprint, ID: strings.Join(parts, "-")}
}

// exceptionFingerprint ports the dashboard's djb2: JavaScript hashes UTF-16 code units with
// int32 wraparound, and Math.abs of int32 min needs 64 bits.
func exceptionFingerprint(identifier string) string {
	var hash int32 = 5381
	for _, unit := range utf16.Encode([]rune(identifier)) {
		hash = hash<<5 + hash + int32(unit)
	}
	abs := int64(hash)
	if abs < 0 {
		abs = -abs
	}
	return fmt.Sprintf("%08x", abs)
}
