package logs

import (
	"os"
	"strconv"
	"strings"
)

const chunkingDebugEnvVar = "LAST9_DEBUG_CHUNKING"

func chunkingDebugEnabled() bool {
	value := strings.TrimSpace(os.Getenv(chunkingDebugEnvVar))
	if value == "" {
		return false
	}

	enabled, err := strconv.ParseBool(value)
	return err == nil && enabled
}
