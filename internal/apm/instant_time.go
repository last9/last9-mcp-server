package apm

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"time"

	"last9-mcp/internal/utils"
)

func (args PromqlInstantQueryArgs) evaluationTime() (int64, error) {
	if len(args.Time) == 0 {
		return resolveInstantQueryTime(args.TimeISO, args.LookbackMinutes)
	}
	value := string(args.Time)
	if args.Time[0] == '"' {
		if err := json.Unmarshal(args.Time, &value); err != nil {
			return 0, err
		}
	}
	instant, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		// Round toward the past so a decimal just below a whole second stays in that second.
		numeric, _, err := big.ParseFloat(value, 10, 128, big.ToNegativeInf)
		if err != nil || numeric.IsInf() {
			return 0, errors.New("time must be RFC3339 or Unix seconds")
		}
		whole, accuracy := numeric.Int64()
		if whole < math.MinInt64/int64(time.Second) || whole > math.MaxInt64/int64(time.Second) {
			return 0, errors.New("time is outside the supported evaluation range")
		}
		if accuracy == big.Above {
			whole--
		}
		instant = time.Unix(whole, 0)
	}
	seconds := instant.Unix()
	if seconds == 0 || seconds > math.MaxInt64/int64(time.Second) || seconds < math.MinInt64/int64(time.Second) {
		return 0, errors.New("time is outside the supported evaluation range or resolves to reserved Unix second zero")
	}
	if args.TimeISO != "" {
		canonical, err := utils.ParseToolTimestamp(args.TimeISO)
		if err != nil {
			return 0, err
		}
		if canonical.Unix() != seconds {
			return 0, errors.New("conflicting time and time_iso: supply the same evaluation second")
		}
	}
	return seconds, nil
}
