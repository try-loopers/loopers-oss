package mcp

type Config struct {
	Enabled         bool                 `mapstructure:"enabled"`
	MaxRequestSize  int64                `mapstructure:"max_request_size"`
	AllowedMethods  []string             `mapstructure:"allowed_methods"`
	Servers         []ServerConfig       `mapstructure:"servers"`
	CircuitBreaker  CircuitBreakerConfig `mapstructure:"circuit_breaker"`
	Sanitizer       SanitizerConfig      `mapstructure:"sanitizer"`
	Inspector       InspectorConfig      `mapstructure:"inspector"`
	MemoizeReadOnly bool                 `mapstructure:"memoize_read_only"`
	GhostTokens     GhostTokensConfig    `mapstructure:"ghost_tokens"`
}

type InspectorConfig struct {
	Enabled                 bool     `mapstructure:"enabled"`
	QuarantineDuration      string   `mapstructure:"quarantine_duration"`
	CustomInjectionPatterns []string `mapstructure:"custom_injection_patterns"`
}

type GhostTokensConfig struct {
	Enabled        bool  `mapstructure:"enabled"`
	ThresholdBytes int64 `mapstructure:"threshold_bytes"`
	TTLSeconds     int   `mapstructure:"ttl_seconds"`
}

type SanitizerConfig struct {
	MaxDescriptionLength int                 `mapstructure:"max_description_length"`
	ToolAllowlist        map[string][]string `mapstructure:"tool_allowlist"`
	MinifySchemas        bool                `mapstructure:"minify_schemas"`
	DeduplicateRefs      bool                `mapstructure:"deduplicate_refs"`
}

type ServerConfig struct {
	Name string `mapstructure:"name"`
	URL  string `mapstructure:"url"`
}

type CircuitBreakerConfig struct {
	Enabled       bool `mapstructure:"enabled"`
	Threshold     int  `mapstructure:"threshold"`
	WindowSeconds int  `mapstructure:"window_seconds"`
}
