package traces

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"last9-mcp/internal/deeplink"
)

// exceptionUnknownLabel is the Discover Exceptions dashboard's label for a missing value; the
// fingerprint hashes it, so it must stay byte-identical to the dashboard's.
const exceptionUnknownLabel = "Unknown"

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

// exceptionLinkRow narrows the page to the row's environment, Not Set included, so the link stays
// valid whatever environment the reader last picked; the narrowed page drops the id's env suffix.
func exceptionLinkRow(e exceptionAggregate) deeplink.ExceptionRow {
	return deeplink.ExceptionRow{ID: identifyException(e, false).ID, Env: e.DeploymentEnvironment}
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
