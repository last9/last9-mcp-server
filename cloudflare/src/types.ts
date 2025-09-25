/**
 * Configuration interface for Last9 MCP Server
 */
export interface Config {
  /** Last9 API authentication token */
  authToken: string;
  /** Last9 base URL */
  baseURL: string;
  /** Refresh token for authentication */
  refreshToken: string;
  /** Organization slug */
  orgSlug?: string;
  /** API base URL */
  apiBaseURL?: string;
  /** Access token for API requests */
  accessToken?: string;
  /** Prometheus read URL */
  prometheusReadURL?: string;
  /** Prometheus username */
  prometheusUsername?: string;
  /** Prometheus password */
  prometheusPassword?: string;
}

/**
 * Environment interface for Cloudflare Workers
 */
export interface Env {
  LAST9_BASE_URL: string;
  LAST9_AUTH_TOKEN: string;
  LAST9_REFRESH_TOKEN: string;
  OAUTH_CLIENT_ID?: string;
  OAUTH_CLIENT_SECRET?: string;
  ENVIRONMENT?: string;
}

/**
 * Service summary response structure
 */
export interface ServiceSummary {
  throughput: number;
  errorRate: number;
  responseTime: number;
  serviceName: string;
  env: string;
}

/**
 * Prometheus instant query response
 */
export interface PromInstantResponse {
  metric: Record<string, string>;
  value: [number, string];
}

/**
 * Prometheus range query response
 */
export interface PromRangeResponse {
  metric: Record<string, string>;
  values: Array<[number, string]>;
}

/**
 * Log entry structure
 */
export interface LogEntry {
  timestamp: string;
  message: string;
  severity?: string;
  service?: string;
  [key: string]: any;
}

/**
 * Trace entry structure
 */
export interface TraceEntry {
  traceId: string;
  spanId: string;
  spanName: string;
  duration: number;
  timestamp: string;
  status: string;
  [key: string]: any;
}

/**
 * Exception entry structure
 */
export interface ExceptionEntry {
  timestamp: string;
  message: string;
  type: string;
  stackTrace?: string;
  service?: string;
  [key: string]: any;
}

/**
 * Alert configuration structure
 */
export interface AlertConfig {
  id: string;
  name: string;
  description: string;
  state: string;
  severity: string;
  query: string;
  [key: string]: any;
}

/**
 * Active alert structure
 */
export interface ActiveAlert {
  id: string;
  ruleId: string;
  ruleName: string;
  state: string;
  severity: string;
  startsAt: string;
  endsAt?: string;
  [key: string]: any;
}

/**
 * Drop rule structure
 */
export interface DropRule {
  name: string;
  filters: DropRuleFilter[];
}

export interface DropRuleFilter {
  key: string;
  value: string;
  operator: 'equals' | 'not_equals';
  conjunction: 'and';
}