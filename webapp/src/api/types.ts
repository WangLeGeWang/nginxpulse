export interface WebsiteInfo {
  id: string;
  name: string;
}

export interface WebsitesResponse {
  websites: WebsiteInfo[];
}

export interface AppStatusResponse {
  log_parsing: boolean;
  log_parsing_progress?: number;
  log_parsing_estimated_total_seconds?: number;
  log_parsing_estimated_remaining_seconds?: number;
  ip_geo_parsing?: boolean;
  ip_geo_pending?: boolean;
  ip_geo_progress?: number;
  ip_geo_estimated_remaining_seconds?: number;
  demo_mode?: boolean;
  language?: string;
  version?: string;
  git_commit?: string;
  migration_required?: boolean;
  setup_required?: boolean;
  config_readonly?: boolean;
}

export interface SourceConfig {
  [key: string]: any;
}

export interface WebsiteConfig {
  name: string;
  logPath?: string;
  domains?: string[];
  logType?: string;
  logFormat?: string;
  logRegex?: string;
  timeLayout?: string;
  sources?: SourceConfig[];
}

export interface SystemConfig {
  logDestination?: string;
  taskInterval?: string;
  logRetentionDays?: number;
  parseBatchSize?: number;
  ipGeoCacheLimit?: number;
  demoMode?: boolean;
  accessKeys?: string[];
  language?: string;
}

export interface ServerConfig {
  Port?: string;
}

export interface DatabaseConfig {
  driver?: string;
  dsn?: string;
  maxOpenConns?: number;
  maxIdleConns?: number;
  connMaxLifetime?: string;
}

export interface PVFilterConfig {
  statusCodeInclude?: number[];
  excludePatterns?: string[];
  excludeIPs?: string[];
}

export interface ConfigPayload {
  system: SystemConfig;
  server: ServerConfig;
  database: DatabaseConfig;
  websites: WebsiteConfig[];
  pvFilter: PVFilterConfig;
}

export interface FieldError {
  field: string;
  message: string;
}

export interface ConfigValidationResult {
  errors: FieldError[];
  warnings: FieldError[];
}

export interface ConfigResponse {
  config: ConfigPayload;
  readonly: boolean;
  setup_required: boolean;
}

export interface ConfigSaveResponse {
  success: boolean;
  restart_required?: boolean;
}

export interface TimeSeriesStats {
  labels: string[];
  visitors: number[];
  pageviews: number[];
}

export interface SimpleSeriesStats {
  key: string[];
  uv: number[];
  uv_percent?: number[];
  pv?: number[];
  pv_percent?: number[];
}

export interface RealtimeSeriesItem {
  name: string;
  count: number;
  percent: number;
}

export interface RealtimeStats {
  activeCount: number;
  activeSeries: number[];
  deviceBreakdown: RealtimeSeriesItem[];
  referers: RealtimeSeriesItem[];
  pages: RealtimeSeriesItem[];
  entryPages: RealtimeSeriesItem[];
  browsers: RealtimeSeriesItem[];
  locations: RealtimeSeriesItem[];
}

export interface IPGeoAnomalyResponse {
  has_issue: boolean;
  count: number;
  samples?: string[];
}

export type ApiResponse<T> = T;
