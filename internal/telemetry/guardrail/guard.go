// Package guardrail checks the scan cost of a log or trace query before it
// runs: range cap, scope, per-query and per-session scan budgets.
package guardrail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"last9-mcp/internal/last9api"
	apilogs "last9-mcp/internal/last9api/logs"
	apitraces "last9-mcp/internal/last9api/traces"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

// estimateTimeout bounds the pre-flight estimate: a slow estimate must not
// become a slow query.
const estimateTimeout = 2 * time.Second

type Signal string

const (
	Logs   Signal = "logs"
	Traces Signal = "traces"
)

// Query is the guarded call: its pipeline and window exactly as they will be sent.
type Query struct {
	Tool     string
	Signal   Signal
	Pipeline []map[string]any
	StartMs  int64
	EndMs    int64
	Index    string
}

type estimator func(context.Context, Query) (last9api.QueryEstimate, error)

type Guard struct {
	limits          models.QueryGuardrail
	estimate        estimator
	estimateTimeout time.Duration
	sessions        *sessions
}

func New(client *http.Client, cfg models.Config) *Guard {
	api := last9api.NewClient(client, cfg)
	return &Guard{
		limits: cfg.QueryGuardrail,
		estimate: func(ctx context.Context, q Query) (last9api.QueryEstimate, error) {
			return estimateQuery(ctx, api, q)
		},
		estimateTimeout: estimateTimeout,
		sessions:        processSessions,
	}
}

// Verdict is the guardrail's answer for one query. Blocked means the query
// must not run; Hints go into the result envelope either way.
type Verdict struct {
	Decision Decision
	Blocked  bool
	Hints    []string
}

// Check never fails: when the estimate is unavailable only the range cap
// runs, on the locally detected scope; the scope threshold and the budgets
// need the estimate's bytes and are skipped.
func (g *Guard) Check(ctx context.Context, req *mcp.CallToolRequest, q Query) Verdict {
	var v Verdict
	mode, err := ParseMode(g.limits.Mode)
	if err != nil {
		// Validate rejects a bad mode at startup, so this one was set after it: fail closed.
		slog.Error("query guardrail mode invalid; enforcing", "tool", q.Tool, "err", err)
		mode = ModeEnforce
		v.Hints = append(v.Hints, hint(ReasonInvalidMode, "", fmt.Sprintf("The configured guardrail mode %q is invalid, so the checks ran in enforce mode.", g.limits.Mode)))
	}
	if mode == ModeOff {
		return Verdict{Decision: Decision{Action: Allow}}
	}

	s := scan{Range: time.Duration(q.EndMs-q.StartMs) * time.Millisecond, Scoped: PipelineScoped(q.Pipeline)}
	if est, err := g.fetchEstimate(ctx, q); err != nil {
		slog.Info("query guardrail estimate unavailable", "tool", q.Tool, "mode", mode, "err", err)
		v.Hints = append(v.Hints, hint(ReasonEstimateUnavailable, "", "The scan estimate could not be fetched, so only the range cap was checked; the scope threshold and the scan and session budgets were not."))
	} else {
		s.Estimate = &est.Bytes
		s.ServerScoped = est.Scoped
	}

	d := decide(g.limits, s)
	v.Blocked = d.Action == Reject && mode == ModeEnforce
	if !v.Blocked && s.Estimate != nil && g.limits.SessionBudgetBytes > 0 {
		if key, ok := sessionKey(req); ok {
			used := g.sessions.add(key, *s.Estimate)
			if d.Action == Allow {
				d = sessionDecision(g.limits.SessionBudgetBytes, used)
			}
		}
	}
	v.Decision = d
	if d.Action != Allow {
		logDecision(mode, q, s, v)
		v.Hints = append([]string{decisionHint(mode, d)}, v.Hints...)
	}
	return v
}

func (g *Guard) fetchEstimate(ctx context.Context, q Query) (last9api.QueryEstimate, error) {
	ctx, cancel := context.WithTimeout(ctx, g.estimateTimeout)
	defer cancel()
	est, err := g.estimate(ctx, q)
	if err != nil {
		return est, err
	}
	if est.Bytes < 0 {
		return est, fmt.Errorf("negative scan estimate %d", est.Bytes)
	}
	return est, nil
}

func estimateQuery(ctx context.Context, c *last9api.Client, q Query) (last9api.QueryEstimate, error) {
	switch q.Signal {
	case Logs:
		return apilogs.EstimateQueryRange(ctx, c, apilogs.EstimateQueryRangeInput{
			Pipeline: q.Pipeline, StartMs: q.StartMs, EndMs: q.EndMs, Index: q.Index,
		})
	case Traces:
		return apitraces.EstimateQueryRange(ctx, c, apitraces.EstimateQueryRangeInput{
			Pipeline: q.Pipeline, StartMs: q.StartMs, EndMs: q.EndMs,
		})
	}
	return last9api.QueryEstimate{}, errors.New("unknown signal " + string(q.Signal))
}

func logDecision(mode string, q Query, s scan, v Verdict) {
	estimate := int64(-1)
	if s.Estimate != nil {
		estimate = *s.Estimate
	}
	slog.Info("query guardrail decision",
		"tool", q.Tool,
		"mode", mode,
		"action", v.Decision.Action,
		"reason", v.Decision.Reason,
		"blocked", v.Blocked,
		"estimate_bytes", estimate,
		"range", s.Range.String(),
		"scoped", s.Scoped,
		"server_scoped", s.ServerScoped,
	)
}

func decisionHint(mode string, d Decision) string {
	switch {
	case d.Action == Reject && mode == ModeShadow:
		return hint(d.Reason, "would be rejected in enforce mode; this query ran", d.Guidance)
	case d.Action == Reject:
		return hint(d.Reason, "rejected", d.Guidance)
	}
	return hint(d.Reason, "", d.Guidance)
}

func hint(reason, outcome, guidance string) string {
	if outcome != "" {
		return fmt.Sprintf("query_guardrail[%s] %s: %s", reason, outcome, guidance)
	}
	return fmt.Sprintf("query_guardrail[%s]: %s", reason, guidance)
}

// AddHints copies the verdict's hints into a tool's result envelope.
func (v Verdict) AddHints(env *utils.ResultEnvelope) {
	for _, h := range v.Hints {
		env.Hint(h)
	}
}

// RejectionResult is the tool error returned instead of running a blocked query.
func (v Verdict) RejectionResult() *mcp.CallToolResult {
	var env utils.ResultEnvelope
	v.AddHints(&env)
	text := fmt.Sprintf("Query not run: the query guardrail rejected it (%s). %s", v.Decision.Reason, v.Decision.Guidance)
	result := utils.EnvelopeToolResult(nil, text, env)
	result.IsError = true
	return result
}
