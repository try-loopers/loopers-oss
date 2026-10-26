---
id: config
title: Configuration Reference
sidebar_label: Configuration File
description: Complete loopers.yaml configuration reference.
---

# Configuration Reference (loopers.yaml)

The loopers.yaml file is the main configuration file for the Loopers proxy server. You can generate a template config file with:

```bash
loopers init
```

## Example Configuration

```yaml
# loopers.yaml
server:
  port: 8080
  admin_port: 9090
  log_level: info          # debug, info, warn, or error
  read_timeout: 30s
  write_timeout: 120s      # increase for long streaming responses
  max_payload_bytes: 2097152 # 2MB maximum request body size

redis:
  addr: "localhost:6379"
  password: ""
  db: 0
  max_retries: 3
  dial_timeout: 5s
  read_timeout: 2s
  write_timeout: 2s

proxy:
  upstream_timeout: 300s    # maximum time to wait for upstream response

session:
  max_per_key: 0            # maximum concurrent active sessions per API key (0 = disabled)

loop_detection:
  enabled: true
  fingerprint:
    threshold: 3            # repeat prompt count before flagging as loop
    window_seconds: 60      # time window for loop detection in seconds
    similarity_threshold: 0.95 # min Jaccard similarity threshold for bi-gram matching
    defeat_padding: false   # truncate large string values (>256 chars) to prevent semantic padding
  velocity:
    max_rps: 0              # maximum requests per second (0=disabled)
    max_endpoint_repeats: 0 # max hits to the same path
    repeat_window_seconds: 60
  stall:
    min_hamming_distance: 0 # distance required to be considered 'progressing'
    low_diversity_threshold: 5
    action: "warn"          # 'warn' or 'block'

mcp:
  enabled: true
  max_request_size: 1048576 # 1MB limit for MCP requests
  servers:
    - name: "mock-server"
      url: "http://mcp-server:3001"
  circuit_breaker:
    enabled: true
    threshold: 5            # repeat tool call block threshold
    window_seconds: 60      # time window for circuit breaker
  sanitizer:
    max_description_length: 512
    tool_allowlist: ["mock-server"]
  inspector:
    enabled: true
    quarantine_duration: "1h"
    custom_injection_patterns: []

risk_profile:
  enabled: true
  ttl: "0"                           # "0" = permanent (no expiry). E.g. "720h" = 30 days.
  auto_quarantine_threshold: 75      # Score above this triggers 1-hour auto-quarantine.
  permanent_block_threshold: 90      # Score above this blocks the agent permanently.

alerting:
  webhook_url: "https://example.com/webhook"
  thresholds:
    - percent: 80
      message: "Budget 80% consumed — approaching limit"

policy:
  enabled: true
  policy_dir: "./policies"
  default_action: "deny"
  signature:
    enabled: false
    type: "hmac"
    secret: ""

rate_limit:
  enabled: true
  requests_per_minute: 60

otel:
  enabled: true
  endpoint: "localhost:4317"
  protocol: "grpc"
  sampling_rate: 1.0


providers:
  openai:
    base_url: https://api.openai.com
  anthropic:
    base_url: https://api.anthropic.com
  gemini:
    base_url: https://generativelanguage.googleapis.com
```

## Options Reference

### server

| Key | Default | Description |
|---|---|---|
| port | 8080 | Port that the proxy listens on |
| admin_host | 127.0.0.1 | Host interface for the admin/metrics server. Set to 0.0.0.0 in Docker/K8s. |
| admin_port | 9090 | Port that the admin and metrics server listens on |
| log_level | info | Logging verbosity |
| read_timeout | 30s | HTTP read timeout |
| write_timeout | 120s | HTTP write timeout (set higher for streaming) |
| max_payload_bytes | 2097152 | Maximum request body size in bytes (2MB default) |

### redis

| Key | Default | Description |
|---|---|---|
| addr | "localhost:6379" | Redis connection address |
| password | "" | Redis password |
| db | 0 | Redis database number |
| max_retries | 3 | Connection retry attempts |
| dial_timeout | 5s | Connection timeout |

### proxy

| Key | Default | Description |
|---|---|---|
| upstream_timeout | 300s | Upstream provider timeout |

### session

| Key | Default | Description |
|---|---|---|
| max_per_key | 0 | The maximum number of concurrent active sessions allowed per API key (0 = disabled) |
| allow_client_budget_override | false | Allow clients to override session limits and budget via HTTP headers |
| drift_detection.enabled | true | Enable multi-turn conversation drift and goal hijacking detection |
| drift_detection.min_turns | 3 | Minimum turns in a session before evaluating drift |
| drift_detection.anchor_similarity_threshold | 0.08 | Minimum containment similarity against initial session anchor |
| drift_detection.drift_score_threshold | 0.45 | Drift score threshold (0.0 to 1.0) above which drift is flagged |

### loop_detection

| Key | Default | Description |
|---|---|---|
| enabled | true | Enable agent loop detection |
| fingerprint.threshold | 3 | Repeat count before loop detection fires |
| fingerprint.window_seconds | 60 | Rolling window for loop detection in seconds |
| fingerprint.similarity_threshold | 0.95 | Minimum Jaccard similarity (0.0 to 1.0) on bi-grams to consider two requests identical |
| fingerprint.defeat_padding | false | Enables structural truncation of large JSON strings (>256 chars) to defeat semantic padding bypasses |
| velocity.max_rps | 0.0 | Maximum requests per second allowed per session (0 = disabled) |
| velocity.max_endpoint_repeats | 0 | Max requests to the same endpoint in a window |
| velocity.repeat_window_seconds | 0 | Window size for endpoint repeat tracking |
| stall.min_hamming_distance | 0 | Distance required between hashes to be considered 'progressing' |
| stall.low_diversity_threshold | 5 | How many sequential low-diversity requests trigger a stall |
| stall.action | "warn" | Action when stalled (`warn` or `block`) |

### alerting

| Key | Default | Description |
|---|---|---|
| webhook_url | "" | Webhook URL for POSTing structured security events |
| thresholds | [] | List of percent thresholds and messages for budget alerts |

### otel

| Key | Default | Description |
|---|---|---|
| enabled | false | Enable OpenTelemetry tracing (EU AI Act tracing) |
| endpoint | "localhost:4317" | OTLP collector endpoint |
| protocol | "grpc" | Transport protocol (`grpc`, `http`, or `stdout`) |
| sampling_rate | 1.0 | Probabilistic sampling rate for successful requests. Enforcement events are always traced at 100%. |

### mcp

| Key | Default | Description |
|---|---|---|
| `enabled` | `false` | Enable MCP JSON-RPC proxy and governance |
| `max_request_size` | `1048576` | Max body size for MCP JSON-RPC requests (1MB default) |
| `servers[].name` | | Name of the upstream MCP server |
| `servers[].url` | | HTTP URL of the upstream MCP server |
| `circuit_breaker.enabled` | `true` | Enable the MCP tool circuit breaker |
| `circuit_breaker.threshold` | `5` | Repetition threshold for identical tool calls |
| `circuit_breaker.window_seconds` | `60` | Time window in seconds for the tool circuit breaker |
| `sanitizer.max_description_length` | `512` | Max length of a string in a tool response |
| `sanitizer.tool_allowlist` | `[]` | List of allowed tool names (empty = allow all) |
| `sanitizer.minify_schemas` | `false` | Enable stateless tool definition minification and description truncation |
| `memoize_read_only` | `false` | Cache deterministic, read-only MCP tool responses in Redis (Blast Radius `<= 15`) |
| `ghost_tokens.enabled` | `false` | Enable ephemeral side-channel encrypted offloading of bulky tool results |
| `ghost_tokens.threshold_bytes` | `8192` | Payload size threshold above which tool results are offloaded as Ghost Stubs |
| `ghost_tokens.ttl_seconds` | `1800` | Redis time-to-live for encrypted ghost token blobs |
| `inspector.enabled` | `false` | Enable synchronous tool response inspection (indirect prompt injection and secret leakage protection) |
| `inspector.quarantine_duration` | `"1h"` | Duration to quarantine an agent key if secrets are detected in a tool response |
| `inspector.custom_injection_patterns` | `[]` | Operator-defined plain-string patterns to flag as injection attempts |

### risk_profile

| Key | Default | Description |
|---|---|---|
| `enabled` | `true` | Enable persistent cross-session agent behavioral risk profiles |
| `ttl` | `"0"` | Profile TTL in Redis (`"0"` for permanent/no-expiry, e.g. `"720h"` for 30 days) |
| `auto_quarantine_threshold` | `75` | Risk score above which agent is automatically placed in 1-hour quarantine lockout |
| `permanent_block_threshold` | `90` | Risk score above which agent is permanently blocked until manual administrative review |

### policy

| Key | Default | Description |
|---|---|---|
| `enabled` | `false` | Enable the embedded OPA/Rego policy engine |
| `policy_file` | `""` | Path to declarative YAML Policy Card file |
| `policy_dir` | `"./policies"` | Local directory containing `.rego` policy files |
| `presets` | `[]` | Built-in policy presets (`"safety"`, `"safety_drift"`, `"pci"`, `"mcp_sandbox"`, `"zero_trust"`, `"owasp_llm_top10"`, `"nist_ai_rmf"`, `"eu_ai_act"`) |
| `default_action` | `"deny"` | Default decision when no rule matches (`"allow"` or `"deny"`) |
| `signature.enabled` | `false` | Enable cryptographic inline signatures for outgoing request bodies |
| `signature.type` | `"hmac"` | Signature type: `"hmac"` (HMAC-SHA256) or `"ed25519"` (Ed25519 signatures) |
| `signature.secret` | `""` | Key filepath, PEM block, or HMAC raw secret key (or leave empty for transient key generation) |

### rate_limit

| Key | Default | Description |
|---|---|---|
| `enabled` | `false` | Enable per-key sliding window rate limiting |
| `requests_per_minute` | `60` | Maximum requests allowed per key per minute |

## Pricing Table Configuration (pricing.yaml)

The `pricing.yaml` file defines model costs, cache read/write discounts, and tier mappings used for Smart FinOps routing:

```yaml
providers:
  openai:
    models:
      "gpt-4o":
        input_per_1m: 2.50
        output_per_1m: 10.00
        cache_read_per_1m: 1.25
        tier: "frontier"
        fast_tier: "gpt-4o-mini"
      "gpt-4o-mini":
        input_per_1m: 0.15
        output_per_1m: 0.60
        tier: "fast"
```

| Key | Description |
|---|---|
| `input_per_1m` | Cost per 1,000,000 un-cached input tokens in USD |
| `output_per_1m` | Cost per 1,000,000 output generation tokens in USD |
| `cache_read_per_1m` | Discounted cost per 1M cached prompt tokens (e.g. Anthropic prompt caching, OpenAI prefix caching) |
| `cache_write_per_1m` | Cost per 1M prompt tokens written into cache |
| `tier` | Model tier classification: `"frontier"` (high-reasoning flagship) or `"fast"` (lightweight/low-latency) |
| `fast_tier` | Target lightweight model identifier used by Smart FinOps routing on simple or intermediate turns |

## Environment Variable Overrides

Certain key configuration values can be overridden with environment variables directly:

```bash
SERVER_PORT=9000
REDIS_ADDR=myredis:6379
REDIS_PASSWORD=mypassword
LOG_LEVEL=debug
```
