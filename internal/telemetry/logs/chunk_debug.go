package logs

import "last9-mcp/internal/models"

func chunkingDebugEnabled(cfg models.Config) bool {
	return cfg.DebugChunking
}
