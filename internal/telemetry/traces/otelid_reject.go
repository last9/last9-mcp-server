package traces

import (
	"errors"
	"log/slog"

	"last9-mcp/internal/otelids"
)

func rejectOTelID(tool string, err error) error {
	var idErr *otelids.Error
	if errors.As(err, &idErr) {
		slog.Error("tool input rejected", "tool", tool, "category", idErr.Category)
	}
	return err
}
