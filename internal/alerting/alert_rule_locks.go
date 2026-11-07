package alerting

import (
	"context"
	"fmt"
	"strings"

	"last9-mcp/internal/utils"
)

// lockAlertRule serializes this process's read-then-write calls on one rule, so a
// concurrent patch_alert or update_alert cannot overwrite or replace what this call read.
func lockAlertRule(ctx context.Context, entityID, ruleID string) (unlock func(), err error) {
	unlock, err = utils.LockKey(ctx, "alert_rule:"+utils.FoldKey(entityID)+"/"+utils.FoldKey(ruleID))
	if err != nil {
		return nil, fmt.Errorf("another call was still changing alert rule %s; nothing was changed: %w", ruleID, err)
	}
	return unlock, nil
}

// lockAlertRuleName serializes creates of one rule name in a group, so the duplicate-name
// check and the create cannot interleave with another create of that name.
func lockAlertRuleName(ctx context.Context, entityID string, ruleName *string) (unlock func(), err error) {
	if ruleName == nil || strings.TrimSpace(*ruleName) == "" {
		return func() {}, nil
	}
	unlock, err = utils.LockKey(ctx, "alert_rule_create:"+utils.FoldKey(entityID)+"/"+utils.FoldKey(*ruleName))
	if err != nil {
		return nil, fmt.Errorf("another call was still creating alert rule %q; nothing was created: %w", strings.TrimSpace(*ruleName), err)
	}
	return unlock, nil
}
