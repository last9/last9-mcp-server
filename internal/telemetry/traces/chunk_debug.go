package traces

import (
	"os"
	"strconv"
	"strings"
)

const tracesChunkingDebugEnvVar = "LAST9_DEBUG_CHUNKING"

func chunkingDebugEnabled() bool {
	value := strings.TrimSpace(os.Getenv(tracesChunkingDebugEnvVar))
	if value == "" {
		return false
	}

	enabled, err := strconv.ParseBool(value)
	return err == nil && enabled
}
