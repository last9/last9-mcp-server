package apm

import (
	"time"

	"last9-mcp/internal/utils"
)

func apmEmptyAdvice(subject string, args apmGuidanceArgs, checks ...string) string {
	return utils.EmptyResultAdvice(subject, utils.ResultGuidanceOptions{
		Window:           utils.WindowFromRange(time.Unix(args.startUnix, 0).UTC(), time.Unix(args.endUnix, 0).UTC()),
		ServiceName:      args.serviceName,
		Environment:      args.env,
		DBSystem:         args.dbSystem,
		AdditionalChecks: checks,
	})
}

type apmGuidanceArgs struct {
	serviceName string
	env         string
	dbSystem    string
	startUnix   int64
	endUnix     int64
}
