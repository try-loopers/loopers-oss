# AI Agent Documentation (AGENT_README.md)

**CONTEXT FOR AI AGENTS**: You are interacting with the Loopers repository. This document contains raw architectural constraints, directory structures, module descriptions, and commands. Prioritize this document over the human `README.md` for technical context.

**STRICT CONSTRAINT**: Loopers follows a strict monochromatic black and white theme. Do not use emojis in any UI, documentation, or CLI output.

---

## 1. Project Goal

Loopers is the bare-metal AI Firewall for the Agentic Era. It is written in Go and intercepts requests across 500+ AI models (15 native providers + generic OpenAI-compatible endpoints) and Model Context Protocol (MCP) servers to prevent token overspending, terminate runaway agent loops, inspect MCP tool responses for prompt injections, track persistent agent behavioral risk, and enforce outbound semantic DLP redaction.

---

## 2. Architecture Constraints

- **Zero-Storage Pass-Through**: Provider API keys and payload data are kept in-memory ONLY during the request lifecycle. Never persist keys or payload bodies to disk or database. For stateful multi-turn policies, a capped execution history (transient session buffer) and active FSM state are stored in-memory in Redis but never written to disk.
- **Deterministic FSM Gating**: Define Finite State Machines in YAML Policy Cards to enforce state-dependent tool calling sequences (e.g. `UNAUTHENTICATED -> AUTHENTICATED -> TRANSACTION_ACTIVE`), automatically transitioning state in Redis on allowed actions and gating actions by `session.state`.
- **Local Translation Middleware (Action Canonicalization)**: Native translation layer that normalizes varied provider requests (system prompts, tool definitions, tool calls) across 15+ providers (and generic OpenAI endpoints) into a single, canonical schema for universal OPA policy evaluation.
- **Fail-Closed Guarantee**: If Redis or the proxy fails, it must fail closed to protect the wallet.
- **Atomic Concurrency Control**: Budget checks and rate limiting happen via Redis Lua scripts to prevent TOCTOU (time-of-check to time-of-use) race conditions under high concurrency.
- **Cryptographic Action Receipts**: Outgoing mutated request bodies are cryptographically signed using either symmetric HMAC-SHA256 or asymmetric Ed25519 keys, injecting `X-Loopers-Signature: t=<timestamp>; sig=<hex>; type=<type>` into the headers of both the upstream requests and the downstream client responses. Requires `policy.signature.enabled: true` and either `policy.signature.secret` (HMAC) or key pair (Ed25519) configured in `loopers.yaml`.
- **Outbound Semantic DLP Gate**: Intercepts outbound LLM completion bodies across both non-streaming JSON envelopes and SSE streaming chunks. Masking replaces sensitive PII/network tokens with `***` and recalculates Content-Length headers without breaking JSON syntax; quarantine severs the connection with HTTP 403 or error SSE events, registers 1-hour Redis TTL lockouts, and increments persistent risk score (+30).
- **Sub-150µs ZSP Token Validation**: Ephemeral Agent Delegation JWTs and DPoP (RFC 9449) proofs are verified statelessly in under 150 microseconds.
- **Fail-Safe Self-Correction**: When an OPA policy blocks an MCP tool call, Loopers returns a valid MCP JSON-RPC 2.0 error object at HTTP 200 (code -32001) with header `X-Loopers-Policy-Block: true`, allowing LLMs to receive the denial as tool output and self-correct instead of crashing on HTTP 403.
- **Inline Context Acceleration ("Normal Days" Optimization)**: Side-channel Ghost Token offloading (`internal/proxy/ghost.go`) encrypts bulky tool outputs (>512B) with AES-256-GCM / HKDF into Redis and injects ~80-token Ghost Stubs, compressing input tokens by 65%–75% with conditional tail-hydration on frontier synthesis. Deterministic prefix canonicalizer (`internal/proxy/canonicalizer.go`) stabilizes tool schemas and whitespace for 90% prompt cache hits. Smart fast-tier routing maps simple turns to lightweight models while preserving warm-cache pinning.
- **Shadow Mode**: Set `server.shadow_mode: true` to run the firewall in observation-only mode — logs and records policy violations without blocking traffic. Useful for auditing policies against live workloads before enforcing.

---

## 3. Tech Stack & Requirements

- **Language**: Go 1.26.6+ (toolchain go1.26.6)
- **Cache/Storage**: Redis 7+
- **Proxy Engine**: `net/http/httputil.ReverseProxy`
- **Policy Engine**: Open Policy Agent (OPA) / Rego with dynamic YAML Policy Card transpilation
- **Auth Standards**: OIDC JWTs, DPoP (RFC 9449)
- **Telemetry**: W3C OpenTelemetry OTLP / Prometheus

---

## 4. Directory & Module Structure Map

```
.
├── cmd/
│   ├── loopers/                   # CLI entrypoint (serve, keys, budget, init, version)
│   └── pricing-validator/         # Pricing schema validator CLI
├── internal/                      # Core private Go packages
│   ├── a2a/                       # Agent-to-Agent trust handoff & escalation broker
│   ├── alerting/                  # Webhook alert dispatcher (SIEM, Slack, PagerDuty)
│   ├── budget/                    # Redis Lua script budget engine (minute, hourly, daily, weekly, monthly, session)
│   ├── cache/                     # Memory lease cache & low-latency budget reservation
│   ├── event/                     # OWASP Top 10 for LLMs security event logger
│   ├── inspector/                 # Outbound Semantic DLP gate (PII regexes, Luhn check, network indicators, JSON completions) & tool response inspection
│   ├── keyring/                   # Proxy key store & identity metadata (--agent-name, --owner, --allowed-tools, etc.)
│   ├── logging/                   # Zero-allocation structured loggers
│   ├── loop/                      # Loop detection engine v1.1 (Bi-Gram Jaccard token similarity, Velocity Limiter, Stall Detector)
│   ├── mcp/                       # MCP JSON-RPC 2.0 proxy, tool cost tracking, Blast Radius prevention, self-correction formatting
│   ├── netutil/                   # Network & TLS utilities
│   ├── otel/                      # OpenTelemetry W3C OTLP sampler & exporter (EU AI Act compliance)
│   ├── policy/                    # Embedded OPA engine with dynamic YAML transpiler, Deterministic FSM Gating, hot-reload, and stateful context
│   ├── pricing/                   # Dynamic remote pricing fetcher & token price store
│   ├── provider/                  # 15 AI provider implementations + Generic BYO OpenAI endpoint
│   │   ├── anthropic/             # Anthropic Messages & Text completion proxy
│   │   ├── azure/                 # Azure OpenAI deployment proxy
│   │   ├── bedrock/               # AWS Bedrock runtime proxy
│   │   ├── cohere/                # Cohere model proxy
│   │   ├── deepseek/              # DeepSeek API proxy
│   │   ├── fireworks/             # Fireworks AI model proxy
│   │   ├── gemini/                # Google Gemini API proxy
│   │   ├── generic/               # Generic OpenAI-compatible endpoint proxy (vLLM, LocalAI, OpenRouter)
│   │   ├── groq/                  # Groq fast inference proxy
│   │   ├── mistral/               # Mistral AI API proxy
│   │   ├── ollama/                # Local Ollama runner proxy
│   │   ├── openai/                # OpenAI Chat Completions & Embeddings proxy
│   │   ├── together/              # Together AI API proxy
│   │   ├── vllm/                  # vLLM local instance proxy
│   │   └── xai/                   # xAI Grok API proxy
│   ├── proxy/                     # Reverse proxy core, SSE streaming token counter, Ghost Tokens (ghost.go) & Prefix Canonicalizer (canonicalizer.go)
│   ├── ratelimit/                 # Per-key sliding window rate limiter (atomic Lua)
│   ├── riskprofile/               # Persistent cross-session agent behavioral risk engine (0-100 scoring, auto-quarantine, permanent block, lazy decay)
│   ├── server/                    # HTTP server engine, ZSP OIDC JWT & DPoP middleware, PathAuthWrapper (/lp-xxx/), admin router (/metrics)
│   ├── session/                   # Redis session manager (session budget, max steps, taint flags, transient session trace buffer, FSM state tracking, tool history, multi-turn drift detection & anchor storage, absolute TTL)
│   ├── signature/                 # Asymmetric Ed25519 & HMAC inline signing package
│   ├── syntactic/                 # Layer 3 Syntactic Normalization (TR39 homoglyph resolution, invisible rune stripping, recursive Base64/URL decoding, delimiter collapse)
│   └── verifier/                  # Offline trace verification, sequential replay engine, and FSM trajectory simulation
├── pkg/
│   └── api/                       # Shared API types (PolicyDeniedResponse, MCPJSONRPCErrorResponse)
├── sdk/
│   ├── python/                    # Python SDK (LoopersOpenAI, LoopersAnthropic, CrewAI, AutoGen, LangChain, LlamaIndex adapters)
│   └── ts/                        # TypeScript / Node.js SDK (LoopersOpenAI, onPolicyBlock callback, vitest suite)
├── Documentation/                 # Docusaurus documentation website & blog
├── docs/                          # Architectural specifications
└── examples/                      # Example policies (01_allow_admin.rego, 02_deny_destructive.rego, 03_taint_tracking.rego)
```

---

## 5. Zero-Code Path-Based Integration (`PathAuthWrapper`)

For pre-built agent frameworks (like `opencode`, `codex`, or any OpenAI-compatible CLI tool) that do not allow injecting custom HTTP headers, Loopers provides zero-code path-based authentication:

- **Usage**: Embed the Loopers proxy key in the base URL path, and pass the real provider key as the standard Bearer token:
  ```env
  OPENAI_BASE_URL=http://localhost:8080/lp-xxx/openai/v1
  OPENAI_API_KEY=sk-proj-YOUR_REAL_OPENAI_KEY
  ```
- **Mechanism**: The `PathAuthWrapper` middleware in `internal/server/server.go`:
  1. Inspects `r.URL.Path` for a proxy key prefix (`lp-...` or `eyJ...`).
  2. Extracts the proxy key and sets `Authorization: Bearer <proxy_key>`.
  3. Moves the incoming Bearer key to header `X-Loopers-Provider-Key`.
  4. Rewrites `r.URL.Path` to strip the proxy key (`/lp-xxx/openai/v1/chat/completions` -> `/openai/v1/chat/completions`).

---

## 6. Core CLI Commands

**Initialize Configuration:**
```bash
loopers init
```

**Run Production Server (Requires TLS Config):**
```bash
loopers serve
```

**Run Local Development Server (No TLS):**

macOS / Linux:
```bash
SERVER_INSECURE_DEV=true loopers serve
```

Windows (PowerShell):
```powershell
$env:SERVER_INSECURE_DEV="true"; loopers serve
```

Windows (Command Prompt):
```cmd
set SERVER_INSECURE_DEV=true && loopers serve
```

**Create Proxy Key with Metadata:**
```bash
loopers keys create --name my-app-key --provider openai \
  --agent-name coding-bot \
  --owner secops \
  --allowed-tools "read_file,search_codebase" \
  --allowed-providers "openai,anthropic" \
  --tags "production,finance"
```

**Set Multi-Window Budget Limits:**
```bash
loopers budget set <KEY_HASH> \
  --minute 0.50 \
  --hourly 2.00 \
  --daily 10.00 \
  --weekly 50.00 \
  --monthly 150.00
```

**Check Budget Status:**
```bash
loopers budget status <KEY_HASH>
```

**Diagnose Configuration, Connectivity, and Firewall Security Engines:**
```bash
loopers doctor
```

**Execute CLI Agent with Proxy Injection:**

Required env vars:
- `LOOPERS_PROXY_KEY` — your proxy key (`lp-xxx`)
- `LOOPERS_PROVIDER` — upstream provider (`openai`, `anthropic`, `openrouter`, etc.). Auto-detected from executable name if omitted (supports `aider`, `openhands`, `pi`, `claude`, `gemini`, `codex`, `opencode`, `dsh`, `deepseek-harness`, `deepseek`).

Optional flags: `--model-override <model>`, `--model-map <alias=model,...>`

macOS / Linux:
```bash
export LOOPERS_PROXY_KEY="lp-xxx"
export LOOPERS_PROVIDER="openrouter"
export OPENAI_API_KEY="sk-or-v1-YOUR_KEY"

loopers exec --model-override "google/gemma-2-9b-it:free" -- opencode
```

Windows (PowerShell):
```powershell
$env:LOOPERS_PROXY_KEY="lp-xxx"
$env:LOOPERS_PROVIDER="openrouter"
$env:OPENAI_API_KEY="sk-or-v1-YOUR_KEY"

loopers exec --model-override "google/gemma-2-9b-it:free" -- opencode
```

Windows (Command Prompt):
```cmd
set LOOPERS_PROXY_KEY=lp-xxx
set LOOPERS_PROVIDER=openrouter
set OPENAI_API_KEY=sk-or-v1-YOUR_KEY

loopers exec --model-override "google/gemma-2-9b-it:free" -- opencode
```

**Verify Session Trace Compliance:**
```bash
loopers verify --trace <path-to-trace.json> [flags]
```
Flags:
- `--trace string` (required) — path to execution trace JSON file to verify
- `--policy-file string` — path to declarative YAML Policy Card (e.g. policies.yaml)
- `--policy-dir string` — directory containing custom Rego files
- `--presets strings` — built-in templates to enable (`safety`, `safety_drift`, `pci`, `mcp_sandbox`, `zero_trust`, `owasp_llm_top10`, `nist_ai_rmf`, `eu_ai_act`)
- `--format string` — output format: `pretty` or `json`
- `--fail-on-violation` — exit with code 1 if violations are found (default: true)

---

## 7. 5-Outcome Decision Plane Policy Engine

The Loopers decision plane evaluates policies against incoming LLM/MCP requests. Instead of a binary `allow / deny` verdict, it resolves to one of five actions:

1. **`allow`**: The request is approved and forwarded to the upstream provider unchanged.
2. **`deny`**: The request is immediately rejected. The client receives an HTTP 403 (HTTP proxy) or an HTTP 200 JSON-RPC error (MCP proxy) containing a policy block payload.
3. **`escalate`**: The request is suspended, and an approval ticket is published to Redis Pub/Sub. The proxy polls for approval up to a configurable timeout (defaults to 60s). If approved, it forwards to upstream; if denied or timed out, it fails closed.
4. **`quarantine`**: The matching agent's key is temporarily locked out in Redis for the duration specified (e.g. `quarantine_for: "5m"`). Subsequent requests from this key bypass policy evaluation and are blocked at the auth layer in <2ms.
5. **`transform`**: Sensitive payload fields are mutated on the fly before forwarding upstream. Supports `mask` (replace with `***`) and `redact` (delete field) operations on prompt text, chat completion messages, or MCP tool arguments.

### Precedence Resolution Engine
When multiple rules match a request, Loopers resolves conflicts using the following precedence:
```
deny (5) > quarantine (4) > escalate (3) > transform (2) > allow (1)
```
- Within the same action tier, the highest `severity` (`critical` > `warn` > `info`) wins.
- If multiple `transform` rules match, all field-level transformations are accumulated and applied sequentially.
- If multiple `quarantine` rules match, the longest `quarantine_for` duration is selected.
- All matching rule names are aggregated into the `evidence` field in logs and traces.

### Declarative YAML Policy Schema
```yaml
version: "1"
metadata:
  name: security-policy-card
rules:
  - name: block-credentials
    match:
      type: llm_call
    conditions:
      - field: prompt_text
        op: matches_regex
        value: "(?i)secret_key"
    action: deny
    reason: "Access denied: credentials leaked"
    severity: critical

  - name: quarantine-threat-agent
    match:
      type: llm_call
    conditions:
      - field: prompt_text
        op: contains
        value: "exploit-payload"
    action: quarantine
    quarantine_for: "15m"
    reason: "Malicious injection attempt"

  - name: escalate-privileged-tools
    match:
      type: mcp_tool_call
      tool: drop_database
    action: escalate
    escalate_to: human
    reason: "Database drop requires human review"

  - name: mask-api-keys
    match:
      type: mcp_tool_call
    conditions:
      - field: arguments.api_key
        op: not_equals
        value: ""
    action: transform
    transforms:
      - field: api_key
        operation: mask
```

### Tool Response Inspection (Indirect Prompt Injection Wall)

For outbound Model Context Protocol (MCP) tool execution responses (`tools/call`), Loopers synchronously buffers and intercepts the payload before it reaches the client. This provides two inline guardrails:

1. **Prompt Injection & Path Traversal (Transform Tier):**
   - Normalizes text (NFKC + zero-width characters stripped) and scans against `inspector.InjectionPatterns` (e.g. `ignore previous instructions`, `forget your instructions`) and `inspector.TraversalPatterns` (e.g. `../../`, `/etc/passwd`).
   - If matched, replaces string values in the JSON-RPC response with `[Content removed: security policy]`, emits a `BlockEvent` with type `response_injection_redacted`, and appends header `X-Loopers-Response-Redacted: true`.
2. **Secret Leakage Prevention (Quarantine Tier):**
   - Scans tool response fields using regular expressions in `inspector.SecretRegexes` (AWS Access Key ID, OpenAI key prefixes, generic JWTs, Slack bot tokens, GitHub PATs, and PEM private keys).
   - If matched, masks the credentials with `***` in-place, writes quarantine flag `loopers:quarantine:<keyHash>` in Redis with the duration set in `mcp.inspector.quarantine_duration`, emits a `QuarantineEvent`, increments persistent risk score by +30, and returns an agent-friendly JSON-RPC error response payload (`api.NewMCPPolicyDeniedResponse`) with header `X-Loopers-Policy-Block: true` at HTTP 200 to allow self-correction.

### Persistent Agent Identity & Behavioral Risk (Capability 3)

Loopers maintains a persistent, cross-session behavioral memory (`AgentRiskProfile`) anchored to the agent's `keyHash` in Redis. This prevents malicious agents from escaping security history by simply rotating session IDs.

```
Redis Key: loopers:risk_profile:{keyHash}
```

#### Deterministic Risk Scoring
Risk scores range from 0 to 100 and are updated atomically via Redis Lua scripts:
- **`+10`**: Policy block (`deny`)
- **`+25`**: Quarantine action triggered (`quarantine`)
- **`+15`**: Escalation action triggered (`escalate`)
- **`+5`**: Sensitive taint flag set (e.g., `secret_accessed`)
- **`+30`**: Secret exfiltration pattern detected in tool response (Capability 2 -> 3 Bridge)
- **`-5`**: Decay per 24 hours of clean inactivity (lazy evaluated on read, min clamp 0)

#### Automated Lockout Thresholds
- **Score > 75 (`auto_quarantine_threshold`)**: Agent is automatically placed under a 1-hour quarantine lockout in Redis.
- **Score > 90 (`permanent_block_threshold`)**: Agent is permanently blocked with HTTP 403 `agent_risk_blocked`, requiring manual administrator review.

#### OPA Policy Integration (`input.agent_risk`)
OPA policies can directly evaluate the persistent risk profile:
```rego
# Block agents with elevated risk scores
deny if {
    input.agent_risk.risk_score > 75
}

# Escalate sensitive actions from agents with a history of accessing secrets
escalate if {
    input.agent_risk.persistent_taint_flags[_] == "secret_accessed"
    input.action.type == "mcp_tool_call"
    input.action.tool_name == "send_email"
}
```

Built-in preset `zero_trust` can be enabled in `policy.presets: ["zero_trust"]`.

---

## 8. AI Agent Resources

The following machine-readable files are available to help AI agents understand and set up this repository without browsing the documentation site:

- **`llms-full.txt`** — single-file complete technical context (setup, CLI, config, headers, OPA policies, providers). Fetch this for full context in one shot: `https://docs.tryloopers.com/llms-full.txt`
- **`llms.txt`** — structured index of all documentation pages with inline quick-reference: `https://docs.tryloopers.com/llms.txt`

For local use (when docs site is not deployed), both files are in `Documentation/static/`.

---

## 9. Outbound Semantic DLP Gate (Capability 4)

Loopers acts as a bidirectional semantic firewall by intercepting outbound LLM completion text across both non-streaming JSON response envelopes and live Server-Sent Events (SSE) streaming token flows.

### Detection Engine (`internal/inspector`)
- **PII Signatures:**
  - RFC 5322 Emails with domain allowlist filtering (`allowed_hosts`).
  - Credit Cards (Visa, MasterCard, Amex, Discover) with strict Luhn checksum validation.
  - US Social Security Numbers (`\b\d{3}-\d{2}-\d{4}\b`).
  - E.164 and NANP Phone Numbers (`(?:\+?1|\b1)?...`).
- **Internal Infrastructure Indicators:**
  - RFC 1918 Private IPs (`10.x`, `172.16-31.x`, `192.168.x`) and loopback (`127.0.0.1`, `localhost`).
  - Internal host suffixes (`.internal`, `.local`, `.corp`).
- **Secret Exfiltration:**
  - AWS Keys, OpenAI/OpenRouter Keys, GitHub PATs, Slack Bot Tokens, JWTs, and PEM Private Keys.

### Action Planes
1. **`mask` (Default for PII):** Redacts sensitive tokens inline with `***`. For non-streaming completions, mutates the provider-specific JSON envelope (OpenAI `choices.message.content` & `tool_calls.arguments`, Anthropic `content.text`, Gemini `candidates.content.parts.text`), recalculates `Content-Length`, and sets header `X-Loopers-DLP-Redacted: true`. For SSE streaming, rewrites individual chunk data payloads in-flight.
2. **`quarantine` (Default for Secrets):** Immediately severs the connection (HTTP 403 or error SSE frame `{"type":"dlp_quarantine"}`), attaches header `X-Loopers-DLP-Block: true`, sets a Redis lockout key (`loopers:quarantine:{keyHash}`, default 1h TTL), updates persistent agent risk score by `+30`, and rejects subsequent agent calls at the authentication gateway.
3. **Streaming Sliding Window:** Maintains a 256-character rolling text window across incoming SSE packets to detect secret patterns fragmented across arbitrary token chunk boundaries.

### Configuration (`loopers.yaml`)
```yaml
server:
  dlp:
    enabled: true
    action: "mask"               # "mask" or "quarantine"
    scan_secrets: true
    scan_pii: true
    scan_network: true
    allowed_hosts:
      - "example.com"
      - "corp.internal"
    quarantine_duration: "1h"
```

---

## 10. Multi-Turn Conversation Drift Detection & Goal Hijacking Protection (Capability 5)

Loopers defends autonomous agent sessions against multi-turn goal hijacking, progressive topic subversion, and sudden prompt injection pivots across long conversational dialogues.

### Mathematical & Algorithmic Engine (`internal/session/drift.go`)
- **Session Anchor ($T_1$):** Atomically persisted in Redis via `SetNX` on the first user prompt at `loopers:session:{keyHash}:{sessionID}:anchor`.
- **Trigram & Stopword Normalization:** Cleans prose, filters top English stopwords, and extracts 3-character character n-grams folded with FNV-1a hashing to `uint16`.
- **Containment Similarity Metric:**
  $$\text{Containment}(A, B) = \frac{|A \cap B|}{|A|}$$
  Evaluates what proportion of the current turn's active vocabulary ($A$) is grounded in the session history ($B$), eliminating false positive Jaccard denominator decay across long dialogues.
- **Dual-Anchor Weighted Continuity:**
  $$\text{Continuity} = \frac{(0.75 \times \text{AnchorSim}) + (0.25 \times \text{PriorTurnSim})}{0.35}$$
  $$\text{DriftScore} = 1.0 - \text{Continuity}$$

### OPA Rego Input Schema (`input.session.drift`)
```rego
input.session.drift.drift_detected       # boolean: true if DriftScore >= threshold and TurnCount >= min_turns
input.session.drift.drift_score          # float: normalized drift score (0.0 = identical, 1.0 = total divergence)
input.session.drift.anchor_similarity    # float: similarity score against initial turn T1
input.session.drift.prior_similarity     # float: similarity score against immediate previous turn
input.session.drift.turn_count           # integer: count of user turns evaluated in this session
```

### Declarative YAML Policy Card Syntax
```yaml
rules:
  - name: block-multi-turn-goal-hijack
    match:
      type: llm_call
    conditions:
      - field: session.drift.drift_detected
        op: equals
        value: true
    action: deny
    severity: critical
    reason: "Blocked: Multi-turn goal hijacking detected. Prompt context has diverged significantly from initial session objective."
```

### Built-in Preset: `safety_drift`
Enable instantly on firewall startup:
```bash
loopers serve --presets safety_drift
```

---

## 9. Syntactic Normalization & Obfuscation Defense (Layer 3)

The Layer 3 syntactic normalizer (`internal/syntactic`) defends against adversarial prompt evasion techniques that attempt to bypass keyword and regex filters by de-obfuscating all incoming prompts and tool responses pre-LLM.

### 5-Stage Transformation Pipeline
1. **Recursive Multi-Layer Decoding:** Unescapes double/triple URL percent-encoding (`%252e%252e` -> `..`), hex escapes (`\xHH`), unicode escapes (`\uHHHH`), and HTML entities (`&#105;`).
2. **Invisible Character Stripping:** Removes 28+ zero-width spaces (`\u200B`, `\u200C`, `\u200D`, `\uFEFF`), bidi directional overrides (`\u202E`), soft hyphens (`\u00AD`), combining marks (`\u034F`), and variation selectors.
3. **Unicode TR39 Homoglyph Canonicalization:** Maps confusable lookalike characters (Cyrillic `а, е, о, р, с, і`, Greek `α, ο, ν, ρ, τ`, Mathematical Alphanumeric bold/italic `𝐢𝐠𝐧𝐨𝐫𝐞`, Fullwidth `ｉｇｎｏｒｅ`, Enclosed runes `ⓘⓖⓝⓞⓡⓔ`) to ASCII equivalents.
4. **Delimiter Padding Collapse & Leetspeak Folding:** Collapses padded token splitting (e.g. `i.g.n.o.r.e`, `i_g_n_o_r_e`, `i-g-n-o-r-e`) and folds leetspeak substitutions (`1gn0r3` -> `ignore`).
5. **Payload Layer Extraction:** `ExtractAllTextLayers()` extracts printable UTF-8 strings embedded inside Base64 blocks to enable deep policy inspection.

### OPA Rego Input Schema (`input.action.obfuscation`)
```rego
input.action.normalized_prompt                 # string: canonical de-obfuscated prompt text
input.action.obfuscation.obfuscation_detected  # boolean: true if any obfuscation was found
input.action.obfuscation.has_homoglyphs        # boolean: true if confusable Unicode characters were resolved
input.action.obfuscation.has_invisible_chars   # boolean: true if zero-width/format runes were stripped
input.action.obfuscation.has_encoding_attacks  # boolean: true if recursive URL/Hex/Unicode encodings were decoded
input.action.obfuscation.has_delim_padding     # boolean: true if intra-word delimiter padding was collapsed
input.action.obfuscation.has_base64_payloads   # boolean: true if embedded Base64 blocks were unpacked
input.action.obfuscation.decoded_layers        # array of strings: all extracted candidate text layers
```

### Declarative Policy Integration
In declarative YAML policy cards, standard rules matching `prompt_text` (with `contains` or `matches_regex`) automatically evaluate against both raw and normalized de-obfuscated layers without duplicating rules:
```yaml
rules:
  - name: block-prompt-injection
    match:
      type: llm_call
    conditions:
      - field: prompt_text
        op: matches_regex
        value: "(?i)(ignore previous|system prompt|override|bypass)"
    action: deny
    severity: critical
    reason: "Blocked: Prompt injection attempt detected (evaluated across all de-obfuscation layers)."
```

---

## 10. Compliance Policy Presets (Layer 6)

Loopers embeds production-ready compliance policy presets that map directly to global AI security and regulatory standards without requiring external policy servers or commercial subscriptions.

### Available Built-in Presets
| Preset Name | Target Standard | Primary Controls Enforced |
|---|---|---|
| `safety` | Core Security Baseline | SSNs, AWS/OpenAI keys, and generic prompt injection regexes. |
| `safety_drift` | Conversational Integrity | Multi-turn conversational drift and goal displacement (`session.drift.drift_detected`). |
| `pci` | PCI-DSS v4.0 | Credit card PANs (Visa, MC, Amex, Discover), CVV/CVC codes, and SQL injection strings. |
| `mcp_sandbox` | Tool Execution Sandboxing | Path traversal in tool arguments (`..`) and bash execution FSM dry-run sequence enforcement. |
| `zero_trust` | Behavioral Governance | Persistent cross-session risk score gating (`agent_risk.risk_score > 75`) and tainted key escalation. |
| `owasp_llm_top10` | OWASP LLM Top 10 (2025) | LLM01 (Prompt Injection & Drift), LLM02 (RCE commands & Path Traversal), LLM06 (DB connection strings & Private keys), LLM08 (Excessive Agency & Destructive tool escalation). |
| `nist_ai_rmf` | NIST AI RMF 1.0 (SP 1270) | GOVERN 1.1 (Anonymous agent rejection), MEASURE 2.7 (Risk score quarantine & Taint isolation), MANAGE 2.4 (IAM & Financial transfer escalation), MANAGE 4.1 (Goal drift containment). |
| `eu_ai_act` | EU AI Act (Reg. 2024/1689) | Art. 5(1)(a/b) Prohibited subliminal/cognitive manipulation, Art. 5(1)(c) Social scoring, Art. 5(1)(d) Remote biometric surveillance, Art. 14 Mandatory human oversight on hiring & credit scoring. |

### Activation Options
1. **Firewall CLI Flag:**
   ```bash
   loopers serve --presets owasp_llm_top10,nist_ai_rmf,eu_ai_act
   ```
2. **Configuration File (`loopers.yaml`):**
   ```yaml
   policy:
     enabled: true
     presets:
       - "owasp_llm_top10"
       - "nist_ai_rmf"
       - "eu_ai_act"
     default_action: "allow"
   ```
3. **Offline Trace Verification:**
   ```bash
   loopers verify --trace ./traces/agent_run.json --presets owasp_llm_top10,nist_ai_rmf,eu_ai_act
   ```

---

## 11. Tool Blast Radius Risk Scoring (Layer 5)

Before executing or approving any Model Context Protocol (MCP) or LLM tool call, Loopers computes a deterministic risk score ($0–100$) and risk tier (`low`, `medium`, `high`, `critical`) based on operational verbs, sensitive targets, external egress vectors, and scope multipliers:

- **Destructive Verbs (+35 pts)**: `delete`, `drop`, `rm`, `purge`, `wipe`, `destroy`, `truncate`, `kill`
- **System Execution (+30 pts)**: `exec`, `execute`, `eval`, `bash`, `sh`, `cmd`, `terminal`, `sudo`
- **IAM & Secrets (+25 pts)**: `iam`, `policy`, `role`, `user_admin`, `secret`, `vault`, `token`, `private_key`
- **Critical Infra & Database (+25 pts)**: `prod`, `database`, `cluster`, `root`, `/etc/passwd`
- **Financial Operations (+25 pts)**: `payment`, `transfer`, `billing`, `payout`, `refund`
- **External Egress (+25 pts)**: URLs (`http://`, `https://`), webhook endpoints, raw IP addresses
- **Bulk & Traversal (+20 pts)**: `*`, `ALL`, `%`, recursive flags (`-rf`), path traversals (`..`)
- **Read-Only Mitigation (-10 pts)**: Safe inspection tools (`get`, `read`, `list`, `search`)

### Declarative Policy Usage:
```yaml
rules:
  - name: escalate-high-blast-radius
    match:
      type: mcp_tool_call
    conditions:
      - field: action.blast_radius
        op: greater_than
        value: "60"
    action: escalate
    escalate_to: human
    severity: warn
    reason: "Tool execution exceeds blast radius threshold (>60)"

  - name: block-critical-blast-radius
    match:
      type: mcp_tool_call
    conditions:
      - field: action.blast_radius_tier
        op: equals
        value: "critical"
    action: deny
    severity: critical
    reason: "Critical blast radius operations are prohibited"
```

---

### Python SDK
```python
from loopers_client import LoopersOpenAI

client = LoopersOpenAI(
    loopers_url="http://localhost:8080",
    loopers_key="lp-xxx",
    provider_key="sk-proj-...",
    session_id="agent-run-1",
    session_budget=5.00,
    max_steps=20
)

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Analyze project logs"}]
)
```

### TypeScript SDK
```typescript
import { LoopersOpenAI } from '@loopers/client';

const client = new LoopersOpenAI({
  loopersUrl: 'http://localhost:8080',
  loopersKey: 'lp-xxx',
  providerKey: 'sk-proj-...',
  sessionId: 'agent-run-1',
  sessionBudget: 5.00,
  maxSteps: 20,
  onPolicyBlock: (error) => {
    console.warn(`Policy Block: ${error.policyName} - ${error.reason}`);
  }
});
```
