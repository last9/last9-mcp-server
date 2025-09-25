import { Config, AlertConfig, ActiveAlert } from '../types';

export const GET_ALERT_CONFIG_DESCRIPTION = `
Get alert configurations (alert rules) from Last9.
Returns all configured alert rules including their conditions, labels, and annotations.
Each alert rule includes:
- id: Unique identifier for the alert rule
- name: Human-readable name of the alert
- description: Detailed description of what the alert monitors
- state: Current state of the alert rule (active, inactive, etc.)
- severity: Alert severity level (critical, warning, info)
- query: PromQL query used for the alert condition
- for: Duration threshold before alert fires
- labels: Key-value pairs for alert routing and grouping
- annotations: Additional metadata and descriptions
- group_name: Alert group this rule belongs to
- condition: Alert condition configuration (thresholds, operators)
- created_at: When the alert rule was created
- updated_at: When the alert rule was last modified
`;

export const GET_ALERTS_DESCRIPTION = `
Get currently active alerts from Last9 monitoring system.
Returns all alerts that are currently firing or have fired recently within the specified time window.
Parameters:
- timestamp: Unix timestamp for the query time (defaults to current time)
- window: Time window in seconds to look back for alerts (defaults to 900 seconds = 15 minutes)

Each alert includes:
- id: Unique identifier for this alert instance
- rule_id: ID of the alert rule that triggered this alert
- rule_name: Name of the alert rule
- state: Current state (firing, resolved, pending)
- severity: Alert severity level
- starts_at: When this alert instance started firing
- ends_at: When this alert instance was resolved (if resolved)
- labels: Key-value pairs for alert identification and routing
- annotations: Additional context and descriptions
- generator_url: URL to the source of the alert
- fingerprint: Unique fingerprint for this alert instance
`;

export async function handleGetAlertConfig(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  try {
    const url = `${config.apiBaseURL}/alert-rules`;

    const response = await fetch(url, {
      method: 'GET',
      headers: {
        'Accept': 'application/json',
        'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
      },
    });

    if (!response.ok) {
      throw new Error(`Failed to get alert config: ${response.status} ${response.statusText}`);
    }

    const data = await response.json();
    const alertRules = data.alert_rules || data.rules || [];

    if (alertRules.length === 0) {
      return {
        content: [{
          type: 'text',
          text: 'No alert rules configured.'
        }]
      };
    }

    const formattedRules: AlertConfig[] = alertRules.map((rule: any) => ({
      id: rule.id,
      name: rule.name || rule.alert_name,
      description: rule.description,
      state: rule.state,
      severity: rule.severity || rule.labels?.severity,
      query: rule.query || rule.expr,
      for: rule.for,
      labels: rule.labels,
      annotations: rule.annotations,
      groupName: rule.group_name,
      condition: rule.condition,
      createdAt: rule.created_at,
      updatedAt: rule.updated_at,
      ...rule
    }));

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(formattedRules, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to get alert config: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}

export async function handleGetAlerts(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  const timestamp = params.timestamp || Math.floor(Date.now() / 1000);
  const window = params.window || 900; // 15 minutes default

  try {
    const url = `${config.apiBaseURL}/alerts/monitor?timestamp=${timestamp}&window=${window}`;

    const response = await fetch(url, {
      method: 'GET',
      headers: {
        'Accept': 'application/json',
        'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
      },
    });

    if (!response.ok) {
      throw new Error(`Failed to get alerts: ${response.status} ${response.statusText}`);
    }

    const data = await response.json();
    const alerts = data.alerts || data.data || [];

    if (alerts.length === 0) {
      return {
        content: [{
          type: 'text',
          text: 'No active alerts found for the specified time window.'
        }]
      };
    }

    const formattedAlerts: ActiveAlert[] = alerts.map((alert: any) => ({
      id: alert.id,
      ruleId: alert.rule_id || alert.ruleId,
      ruleName: alert.rule_name || alert.ruleName || alert.alertname,
      state: alert.state || alert.status,
      severity: alert.severity || alert.labels?.severity,
      startsAt: alert.starts_at || alert.startsAt,
      endsAt: alert.ends_at || alert.endsAt,
      labels: alert.labels,
      annotations: alert.annotations,
      generatorUrl: alert.generator_url || alert.generatorURL,
      fingerprint: alert.fingerprint,
      value: alert.value,
      ...alert
    }));

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(formattedAlerts, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to get alerts: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}