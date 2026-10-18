package alerting

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ValidateAlertExpressionArgs struct {
	EntityID       string            `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID whose KPIs the expression uses"`
	Expression     string            `json:"expression" jsonschema:"(Required) Arithmetic expression over KPI names, for example errors / requests"`
	IndicatorsUsed map[string]string `json:"indicators_used" jsonschema:"(Required) Non-empty map of each KPI name used in the expression to its KPI UUID owned by entity_id, for example {\"errors\":\"<kpi-uuid>\"}; every name in the expression must be present. Use each KPI's exact name: this check accepts any name, but create_alert and update_alert reject other names"`
}

func NewValidateAlertExpressionHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ValidateAlertExpressionArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ValidateAlertExpressionArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return utils.ToolErrorResult("entity_id is required"), nil, nil
		}
		if strings.TrimSpace(args.Expression) == "" {
			return utils.ToolErrorResult("expression is required"), nil, nil
		}
		if len(args.IndicatorsUsed) == 0 {
			return utils.ToolErrorResult("indicators_used is required and must map each indicator name in the expression to its KPI UUID"), nil, nil
		}
		body, err := entities.ValidateAlertExpression(ctx, last9api.NewClient(client, cfg), entities.ValidateExpressionInput{
			EntityID: args.EntityID, Expression: bracketIndicatorNames(args.Expression, args.IndicatorsUsed), IndicatorsUsed: args.IndicatorsUsed,
		})
		var apiErr *utils.UpstreamHTTPError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnprocessableEntity && apiErr.Body != "" {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: apiErr.Body}}}, nil, nil
		}
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
}

var bareVariableName = regexp.MustCompile(`^\p{L}[\p{L}\p{N}_]*$`)

// bracketIndicatorNames writes each indicator name the dry-run's parser (govaluate) cannot
// read bare, such as "Test Alert" or "http.errors", as [name]. Saved rules bind names
// textually, so without this the dry-run rejects expressions create_alert accepts.
func bracketIndicatorNames(expr string, indicators map[string]string) string {
	var names []string
	for name := range indicators {
		if name != "" && !strings.Contains(name, "]") && !parsesBare(name) {
			names = append(names, regexp.QuoteMeta(name))
		}
	}
	if len(names) == 0 {
		return expr
	}
	// Longest first: Go's alternation is leftmost-first, so "a b c" must be tried before "a b".
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	pattern := regexp.MustCompile(strings.Join(names, "|"))
	var out strings.Builder
	last := 0
	for _, m := range pattern.FindAllStringIndex(expr, -1) {
		start, end := m[0], m[1]
		if alreadyBracketed(expr, start, end) || continuesWord(expr, start, end) {
			continue
		}
		out.WriteString(expr[last:start])
		out.WriteString("[" + expr[start:end] + "]")
		last = end
	}
	out.WriteString(expr[last:])
	return out.String()
}

func parsesBare(name string) bool {
	switch name {
	case "true", "false", "in", "IN":
		return false
	}
	return bareVariableName.MatchString(name)
}

func alreadyBracketed(expr string, start, end int) bool {
	return start > 0 && expr[start-1] == '[' && end < len(expr) && expr[end] == ']'
}

// continuesWord reports whether the match is part of a longer variable name, such as
// "my-kpi" inside "my-kpi2".
func continuesWord(expr string, start, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(expr[:start])
	first, _ := utf8.DecodeRuneInString(expr[start:end])
	after, _ := utf8.DecodeRuneInString(expr[end:])
	lastRune, _ := utf8.DecodeLastRuneInString(expr[start:end])
	return (start > 0 && isNameRune(before) && isNameRune(first)) ||
		(end < len(expr) && isNameRune(after) && isNameRune(lastRune))
}

func isNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.'
}
