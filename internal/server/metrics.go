package server

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_requests_total",
		Help: "Total number of AI requests processed by Loopers",
	}, []string{"provider", "model", "status"})

	budgetBlocksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_budget_blocks_total",
		Help: "Total number of AI requests blocked by Loopers budget enforcement",
	}, []string{"provider", "window"})

	loopBlocksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_loop_blocks_total",
		Help: "Total number of requests blocked by the loop detection engine",
	}, []string{"provider", "rule"})

	loopWarnsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_loop_warns_total",
		Help: "Total number of requests flagged with warnings by the loop detection engine",
	}, []string{"provider", "rule"})

	shadowBlockedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_shadow_blocked_total",
		Help: "Total number of AI requests shadow blocked by Loopers budget enforcement",
	}, []string{"provider", "window"})

	spendUSDTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_spend_usd_total",
		Help: "Total spend in USD tracked by Loopers",
	}, []string{"provider", "key_hash"})

	savingsUSDTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_savings_usd_total",
		Help: "Total USD saved via prompt caching and token optimization",
	}, []string{"provider", "key_hash"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopers_request_duration_seconds",
		Help:    "AI request duration in seconds",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0},
	}, []string{"provider"})

	tokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_tokens_total",
		Help: "Total number of tokens processed by Loopers",
	}, []string{"provider", "direction"})

	mcpToolCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_mcp_tool_calls_total",
		Help: "Total MCP tool calls processed",
	}, []string{"tool_name", "status"})

	mcpCircuitBreaksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_mcp_circuit_breaks_total",
		Help: "Total MCP tool calls blocked by circuit breaker",
	}, []string{"tool_name"})

	mcpToolSpendUSD = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_mcp_tool_spend_usd_total",
		Help: "Total USD spent on MCP tool calls",
	}, []string{"tool_name", "key_hash"})

	rateLimitBlocksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_rate_limit_blocks_total",
		Help: "Total number of AI requests blocked by Loopers rate limiting",
	}, []string{"provider"})

	zspAuthFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_zsp_auth_failures_total",
		Help: "Total ZSP JWT validation failures",
	}, []string{"reason"})

	escalationsApprovedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_escalations_approved_total",
		Help: "Total ZSP escalations approved",
	}, []string{"reason"})

	escalationsTimeoutTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_escalations_timeout_total",
		Help: "Total ZSP escalations timed out",
	}, []string{"reason"})

	policyEscalationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_policy_escalations_total",
		Help: "Total requests escalated to human or SaaS control plane by policy",
	}, []string{"provider", "escalate_to"})

	policyQuarantinesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_policy_quarantines_total",
		Help: "Total agent keyHashes quarantined by policy decision",
	}, []string{"provider", "severity"})

	policyTransformsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_policy_transforms_total",
		Help: "Total requests where payload was mutated (transform) by policy",
	}, []string{"provider", "operation"})

	dlpRedactionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_dlp_redactions_total",
		Help: "Total LLM completions where DLP masked sensitive content",
	}, []string{"provider"})

	dlpQuarantinesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopers_dlp_quarantines_total",
		Help: "Total agent keys quarantined due to DLP secret detection in LLM completion",
	}, []string{"provider"})
)
