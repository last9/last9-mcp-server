import { Config, Env } from './types';

/**
 * Extract organization slug from JWT token
 */
export function extractOrgSlugFromToken(accessToken: string): string {
  const claims = extractClaimsFromToken(accessToken);
  const orgSlug = claims.organization_slug;
  if (!orgSlug) {
    throw new Error('Organization slug not found in token');
  }
  return orgSlug;
}

/**
 * Extract claims from JWT token
 */
export function extractClaimsFromToken(accessToken: string): Record<string, any> {
  const parts = accessToken.split('.');
  if (parts.length !== 3) {
    throw new Error('Invalid JWT token format');
  }

  try {
    const payload = atob(parts[1].replace(/-/g, '+').replace(/_/g, '/'));
    return JSON.parse(payload);
  } catch (error) {
    throw new Error('Failed to decode token payload');
  }
}

/**
 * Extract action URL from JWT token
 */
export function extractActionURLFromToken(accessToken: string): string {
  const claims = extractClaimsFromToken(accessToken);
  const aud = claims.aud;

  if (!Array.isArray(aud) || aud.length === 0) {
    throw new Error('No audience found in token claims');
  }

  const audStr = aud[0];
  if (audStr.startsWith('https://') || audStr.startsWith('http://')) {
    return audStr;
  }
  return `https://${audStr}`;
}

/**
 * Refresh access token using refresh token
 */
export async function refreshAccessToken(config: Config): Promise<string> {
  const data = {
    refresh_token: config.refreshToken,
  };

  const actionURL = extractActionURLFromToken(config.refreshToken);
  const oauthURL = actionURL.endsWith('/api') ? actionURL.slice(0, -4) : actionURL;

  const url = `${oauthURL}/api/v4/oauth/access_token`;

  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(data),
  });

  if (!response.ok) {
    throw new Error(`Failed to refresh token: ${response.status}`);
  }

  const result = await response.json();
  return result.access_token;
}

/**
 * Fetch physical index for logs queries using service name and environment
 * Uses an instant query for data from the last 1 day
 */
export async function fetchPhysicalIndex(serviceName: string, env: string, config: Config): Promise<string> {
  // Build the PromQL query with a 1-day window
  let query = `sum by (name, destination) (physical_index_service_count{service_name='${serviceName}'`;
  if (env && env.trim() !== '') {
    query += `,env=~'${env}'`;
  }
  query += '}[1d])';

  // Get current time for the instant query
  const currentTime = Math.floor(Date.now() / 1000);

  try {
    // Make the Prometheus instant query
    const response = await makePromInstantQuery(query, currentTime, config);

    if (!response.ok) {
      return ''; // Return empty string to continue without index
    }

    const result = await response.json();

    // Parse the response to extract the first index
    const data = result.data?.result;
    if (!Array.isArray(data) || data.length === 0) {
      return '';
    }

    // Extract the index name from the first result
    const firstResult = data[0];
    const indexName = firstResult.metric?.name;
    if (indexName) {
      return `physical_index:${indexName}`;
    }

    return '';
  } catch (error) {
    return ''; // Return empty string to continue without index
  }
}

/**
 * Get default region based on Last9 base URL
 */
export function getDefaultRegion(baseURL: string): string {
  let hostname = baseURL;

  // Remove protocol
  if (hostname.startsWith('https://')) {
    hostname = hostname.slice(8);
  } else if (hostname.startsWith('http://')) {
    hostname = hostname.slice(7);
  }

  // Remove port
  const colonIndex = hostname.indexOf(':');
  if (colonIndex !== -1) {
    hostname = hostname.slice(0, colonIndex);
  }

  switch (hostname) {
    case 'otlp.last9.io':
      return 'us-east-1';
    case 'otlp-aps1.last9.io':
      return 'ap-south-1';
    case 'otlp-apse1.last9.io':
      return 'ap-southeast-1';
    default:
      return 'us-east-1';
  }
}

/**
 * Get time range from parameters
 */
export function getTimeRange(
  params: Record<string, any>,
  defaultLookbackMinutes: number = 60
): { startTime: Date; endTime: Date } {
  let endTime = new Date();
  let lookbackMinutes = defaultLookbackMinutes;

  if (typeof params.lookback_minutes === 'number') {
    lookbackMinutes = params.lookback_minutes;
    if (lookbackMinutes < 1) {
      throw new Error('lookback_minutes must be at least 1');
    }
    if (lookbackMinutes > 1440) {
      throw new Error('lookback_minutes cannot exceed 1440 (24 hours)');
    }
  }

  let startTime = new Date(endTime.getTime() - lookbackMinutes * 60000);

  // Override with explicit timestamps if provided
  if (typeof params.start_time_iso === 'string' && params.start_time_iso !== '') {
    const parsed = new Date(params.start_time_iso.replace(' ', 'T') + 'Z');
    if (isNaN(parsed.getTime())) {
      throw new Error('Invalid start_time_iso format');
    }
    startTime = parsed;
  }

  if (typeof params.end_time_iso === 'string' && params.end_time_iso !== '') {
    const parsed = new Date(params.end_time_iso.replace(' ', 'T') + 'Z');
    if (isNaN(parsed.getTime())) {
      throw new Error('Invalid end_time_iso format');
    }
    endTime = parsed;
  }

  return { startTime, endTime };
}

/**
 * Make Prometheus instant query (matches Go implementation)
 */
export async function makePromInstantQuery(
  query: string,
  time: number,
  config: Config
): Promise<Response> {
  if (!config.prometheusReadURL || !config.prometheusUsername || !config.prometheusPassword) {
    throw new Error('Prometheus configuration not available. Make sure datasources are configured.');
  }

  // Use Last9's prom_query_instant API endpoint like Go version
  const url = `${config.apiBaseURL}/prom_query_instant`;
  const requestBody = {
    query,
    timestamp: time,
    read_url: config.prometheusReadURL,
    username: config.prometheusUsername,
    password: config.prometheusPassword,
  };

  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
    },
    body: JSON.stringify(requestBody),
  });

  return response;
}

/**
 * Make Prometheus range query (matches Go implementation)
 */
export async function makePromRangeQuery(
  query: string,
  start: number,
  end: number,
  step: string,
  config: Config
): Promise<Response> {
  if (!config.prometheusReadURL || !config.prometheusUsername || !config.prometheusPassword) {
    throw new Error('Prometheus configuration not available. Make sure datasources are configured.');
  }

  // Use Last9's prom_query API endpoint like Go version
  const url = `${config.apiBaseURL}/prom_query`;
  const requestBody = {
    query,
    timestamp: start,
    window: end - start,
    read_url: config.prometheusReadURL,
    username: config.prometheusUsername,
    password: config.prometheusPassword,
  };

  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
    },
    body: JSON.stringify(requestBody),
  });

  return response;
}

/**
 * Make Prometheus label values query (matches Go implementation)
 */
export async function makePromLabelValuesQuery(
  label: string,
  matches: string,
  start: number,
  end: number,
  config: Config
): Promise<Response> {
  if (!config.prometheusReadURL || !config.prometheusUsername || !config.prometheusPassword) {
    throw new Error('Prometheus configuration not available. Make sure datasources are configured.');
  }

  // Use Last9's prom_label_values API endpoint like Go version
  const url = `${config.apiBaseURL}/prom_label_values`;
  const requestBody = {
    label: label,
    timestamp: start,
    window: end - start,
    read_url: config.prometheusReadURL,
    username: config.prometheusUsername,
    password: config.prometheusPassword,
    matches: [matches],
  };

  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
    },
    body: JSON.stringify(requestBody),
  });

  return response;
}

/**
 * Make Prometheus labels query (matches Go implementation)
 */
export async function makePromLabelsQuery(
  metric: string,
  start: number,
  end: number,
  config: Config
): Promise<Response> {
  if (!config.prometheusReadURL || !config.prometheusUsername || !config.prometheusPassword) {
    throw new Error('Prometheus configuration not available. Make sure datasources are configured.');
  }

  // Use Last9's apm/labels API endpoint like Go version
  const url = `${config.apiBaseURL}/apm/labels`;
  const requestBody = {
    timestamp: start,
    window: end - start,
    read_url: config.prometheusReadURL,
    username: config.prometheusUsername,
    password: config.prometheusPassword,
    metric: metric,
  };

  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
    },
    body: JSON.stringify(requestBody),
  });

  return response;
}

/**
 * Setup configuration from environment
 */
export function setupConfig(env: Env): Config {
  const config: Config = {
    authToken: env.LAST9_AUTH_TOKEN,
    baseURL: env.LAST9_BASE_URL,
    refreshToken: env.LAST9_REFRESH_TOKEN,
  };

  return config;
}

/**
 * Setup datasource configuration by fetching from API (matches Go implementation)
 */
async function setupDataSource(config: Config): Promise<void> {
  if (!config.apiBaseURL || !config.accessToken) {
    throw new Error('API base URL and access token are required for datasource setup');
  }

  const response = await fetch(`${config.apiBaseURL}/datasources`, {
    method: 'GET',
    headers: {
      'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
      'Content-Type': 'application/json',
    },
  });

  if (!response.ok) {
    throw new Error(`Failed to get metrics datasource: ${response.status} ${response.statusText}`);
  }

  const datasources = await response.json() as any[];

  // Find the default datasource
  const defaultDatasource = datasources.find((ds: any) => ds.is_default === true);

  if (!defaultDatasource) {
    throw new Error('Default datasource not found');
  }

  // Extract Prometheus configuration from default datasource
  config.prometheusReadURL = defaultDatasource.url;
  config.prometheusUsername = defaultDatasource.properties?.username;
  config.prometheusPassword = defaultDatasource.properties?.password;

  if (!config.prometheusReadURL || !config.prometheusUsername || !config.prometheusPassword) {
    throw new Error('Default datasource missing required properties (url, username, password)');
  }
}

/**
 * Populate API configuration
 */
export async function populateAPICfg(config: Config): Promise<void> {
  // Refresh access token
  config.accessToken = await refreshAccessToken(config);

  // Extract org slug
  config.orgSlug = extractOrgSlugFromToken(config.accessToken);

  // Set API base URL (matching Go implementation exactly)
  config.apiBaseURL = `https://app.last9.io/api/v4/organizations/${config.orgSlug}`;

  // Fetch Prometheus datasource configuration from API (matching Go implementation)
  await setupDataSource(config);
}