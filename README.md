<p align="center">
  <img src="./docs/cover.png" alt="Loopers" width="800"/>
</p>

# Loopers

### The AI Firewall for the Agentic Era

> **Stop runaway loops. Block data exfiltration. Enforce policy. No SDK required.**

<p align="left">
  <img src="https://img.shields.io/badge/license-MIT-black.svg?style=for-the-badge" alt="License" />
  <img src="https://img.shields.io/badge/go-1.26.6%2B-black.svg?style=for-the-badge" alt="Go Version" />
  <img src="https://img.shields.io/badge/models-500%2B%20Models%20%C2%B7%2015%20Providers-black.svg?style=for-the-badge" alt="Models and Providers" />
  <img src="https://img.shields.io/badge/architecture-Fail--Closed-black.svg?style=for-the-badge" alt="Fail Closed Architecture" />
  <img src="https://img.shields.io/badge/budget%20leakage-0%25%20Over--Charge%20by%20Design-black.svg?style=for-the-badge" alt="Zero Over-Charge Guarantee" />
  <img src="https://img.shields.io/badge/redis-7%2B%20Required-black.svg?style=for-the-badge" alt="Redis 7+ Required" />
</p>

---

## The Problem

**1. Uncontrolled Cost & Runaway Loops**  
Autonomous AI agents are expensive when running unsupervised. A single stuck retry loop, recursive reasoning stall, or cascade failure can burn hundreds of dollars in API credits within minutes. Upstream provider dashboards only give post-hoc metrics and alerts after the damage has already hit your card. Loopers intercepts traffic in real time and cuts off spending before the bill is generated.

**2. Runtime Tool Poisoning & Data Exfiltration**  
Autonomous agents ingest external web pages, parse tool responses, and execute commands. A compromised MCP server, poisoned tool response, or indirect prompt injection can turn your agent into an exfiltration vector. Outbound LLM completions frequently leak credentials, PII, or internal network IPs back to untrusted contexts. Without a runtime firewall inspecting bidirectional traffic, agents operate completely unshielded.

**3. Lack of Deterministic Policy Governance**  
As agent systems scale, relying on prompt instructions for safety and operational boundaries fails. Safety directives placed in system prompts can be overridden by jailbreaks or forgotten across long context windows. Loopers introduces a deterministic, stateful enforcement layer: declarative policies that quarantine, block, mutate, or escalate live requests at the network boundary.

### Origins & Authors

Loopers is created and actively maintained by **[Varad Khoriya](https://github.com/CURSED-ME)** and **[Mayank Agrawal](https://github.com/Mxyank)** (part of the Loopers organization).

The project was born when an autonomous test agent got stuck in a recursive loop and ran up an unexpected bill during local development. After analyzing the root cause, it became clear that major AI platforms only offer passive telemetry rather than active circuit breakers. We built Loopers to establish an open-source, bare-metal firewall standard for autonomous agent systems.

Have questions, suggestions, or need help integrating? Join the community on [GitHub Discussions](https://github.com/try-loopers/loopers-oss/discussions) or submit an issue on [GitHub Issues](https://github.com/try-loopers/loopers-oss/issues).

> **AI Agents & LLMs:** See [AGENT_README.md](./AGENT_README.md) for dense machine-readable context and configuration schemas.

---

## What Loopers Is — and Is Not

Loopers is a **stateful, runtime security firewall** for autonomous AI agents.  
It is **not** an AI gateway, model router, prompt management platform, or agent orchestration framework.

### The Firewall Difference

| Capability | AI Gateways (LiteLLM, PortKey, OpenRouter) | Loopers AI Firewall |
|---|---|---|
| **Primary Focus** | Traffic routing, model aggregation, latency | **Dual-Regime:** Rogue threat defense ("Bad Days") + Inline wire acceleration ("Normal Days") |
| **Budget Enforcement** | Post-request tracking or passive webhook alerts | Pre-call atomic leases with 0% over-charge by design |
| **Streaming Enforcement** | Passes stream chunks transparently | Active chunk-by-chunk SSE inspection; severs connections mid-stream |
| **Context Compression (Ghost Tokens)** | None (raw payloads sent to LLM) | Ephemeral AES-256-GCM side-channel offload; **65%–75% input token reduction** |
| **Prefix Canonicalization** | None | Deterministic tool schema sorting & whitespace normalization for **90% KV cache hits** |
| **Smart Fast-Tier Routing** | Static fallback on error | Dynamic routing of intermediate turns to fast-tier models with warm-cache pinning |
| **Syntactic Normalization** | None | 5-stage de-obfuscation (Unicode homoglyphs, invisible runes, base64 decoding, token collapse) |
| **Outbound DLP Gate** | None | Real-time scanning & redaction of PII, secrets, and private IPs |
| **Agent Behavioral Risk** | Stateless per request | Persistent cross-session risk scoring (0–100) & automated quarantine |
| **Runaway Loop Breaker** | None | Real-time 3-engine detection (Fingerprint, Velocity, Stall) |
| **Multi-Turn Drift Protection** | Stateless per request | Active containment scoring against initial session anchor ($T_1$) |
| **Compliance Presets** | None | Out-of-the-box zero-dependency policies for OWASP LLM Top 10, NIST AI RMF 1.0, and EU AI Act |
| **Tool Blast Radius Scoring** | None | Deterministic risk scoring (0–100) & tier classification on tool verbs, targets, wildcards, and egress |
| **Tool Execution FSM** | None | Stateful tool sequencing (e.g. require dry run before bash execution) |
| **Human-in-the-Loop Escalation** | None | Suspends live HTTP requests for Redis Pub/Sub approval |
| **Zero-Storage Pass-Through** | Often caches prompts / logs payloads | Provider keys & payloads stay strictly in-memory (never persisted) |
| **Failure Mode** | Fail-open for maximum availability | **Fail-closed** by design (blocks traffic if state/Redis drops) |

---

## How It Works

### Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────────┐
│                             Agent Runtime                                │
│       (Aider / OpenHands / Claude Code / Custom Agent / Python SDK)      │
└────────────────────────────────────┬─────────────────────────────────────┘
                                     │ HTTP (e.g., OPENAI_BASE_URL)
                                     ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                        Loopers AI Firewall (:8080)                       │
│                                                                          │
│ ┌──────────────────────────────────────────────────────────────────────┐ │
│ │          REGIME 1: ROGUE AGENT DEFENSE & THREAT MITIGATION           │ │
│ │                     ("Bad Days" Protection)                          │ │
│ │  [1. Key Auth & DPoP] ──► [2. Risk Score & Quarantine Gate]          │ │
│ │  [3. Syntactic Normalizer] ──► [4. Multi-Turn Drift Containment]     │ │
│ │  [5. Pre-Call Budget Lease] ──► [6. OPA/YAML Policy Engine & FSM]    │ │
│ │  [7. Multi-Engine Loop Detector] ──► [8. Outbound DLP & SSE Cutoff]  │ │
│ └──────────────────────────────────────────────────────────────────────┘ │
│                                    │                                     │
│ ┌──────────────────────────────────────────────────────────────────────┐ │
│ │         REGIME 2: INLINE CONTEXT ACCELERATION & WIRE FINOPS          │ │
│ │                    ("Normal Days" Optimization)                      │ │
│ │  [A. Zero-SDK Session Derivation]                                    │ │
│ │  [B. Ghost Token Offload (Redis AES-256-GCM / HKDF)]                 │ │
│ │  [C. Dynamic Tail-Hydration (Frontier Synthesis vs Fast-Tier)]       │ │
│ │  [D. Deterministic Prefix Canonicalizer (KV Cache Maximization)]     │ │
│ │  [E. Smart Fast-Tier Model Routing & Warm Cache Pinning]             │ │
│ └──────────────────────────────────────────────────────────────────────┘ │
│                                    │                                     │
│  Admin Server (:9090): /metrics (Prometheus) | /health                   │
│  Telemetry: OpenTelemetry Traces (OTLP gRPC/HTTP) | HMAC Webhooks        │
└───────────────────┬──────────────────────────────────┬───────────────────┘
                    │                                  │
                    ▼                                  ▼
          ┌───────────────────┐              ┌───────────────────┐
          │     Redis 7+      │              │ Upstream Provider │
          │ (Atomic Leases,   │              │ (OpenAI, Anthropic│
          │  Encrypted Ghosts,│              │  Gemini, Bedrock, │
          │  FSM Session State│              │  DeepSeek, Groq...)│
          │  Quarantine Keys) │              └───────────────────┘
          └───────────────────┘
```

### Request Lifecycle

1. **Key Authentication & DPoP Check:** The agent request arrives with a Loopers proxy key (`lp-...`). Loopers verifies the HMAC-SHA256 hash against Redis. If DPoP is configured, proof-of-possession is validated against the sender's public key.
2. **Quarantine & Risk Profile Gate:** Checks the agent's persistent behavioral risk score. If the score exceeds the quarantine threshold (default: 75), requests are rejected with `403 Forbidden`. If permanently blocked (score > 90), traffic is dropped.
3. **Layer 3 Syntactic Normalization:** Inbound prompt content is processed through the 5-stage syntactic normalization pipeline (`internal/syntactic`), resolving Unicode homoglyphs (Cyrillic, Greek, Math Bold), stripping zero-width/bidi runes, recursively decoding nested Base64/URL percent escapes, and collapsing delimited tokens (`i.g.n.o.r.e`). Both raw and canonical `normalized_prompt` are exposed to the policy engine.
4. **Pre-Call Atomic Budget Lease:** Loopers calculates the maximum estimated cost using model rates from `pricing.yaml` and atomically reserves this quota in Redis across minute, hourly, daily, weekly, and monthly sliding windows. If limits are reached, the call is blocked before hitting the provider.
5. **Inline Context Acceleration & Prefix Canonicalization (Normal Days):** Evaluates multi-turn Ghost Token stubs. On frontier synthesis turns, tail-hydrates raw payloads via `<loopers_hydrated_context>`. Applies deterministic JSON prefix canonicalization (tool schema key sorting, whitespace normalization, timestamp stabilization) to guarantee upstream KV prompt cache hits.
6. **Smart Fast-Tier Routing (Normal Days):** On cold cache, routes lightweight intermediate verification turns to fast models (e.g. `gpt-4o` -> `gpt-4o-mini`, `deepseek-v4-pro` -> `deepseek-v4.1-flash-1017`). On warm cache, pins the model to maintain the 90% prompt cache read discount.
7. **Policy Engine & FSM Evaluation (Bad Days):** Evaluates Open Policy Agent (OPA) Rego rules and declarative YAML Policy Cards against canonical normalized inputs. Validates session state transitions, computes multi-turn conversation drift against initial session anchor goals, and checks tool sequences. If an `escalate` policy triggers, Loopers suspends the live HTTP connection and awaits human/admin approval via Redis Pub/Sub.
8. **Multi-Engine Loop Detection (Bad Days):** Evaluates the request against session history using bi-gram Jaccard similarity, request velocity, and SimHash Hamming distance to detect stuck reasoning or recursive loops.
9. **Upstream Request Forwarding:** Injects the actual upstream provider key (from `X-Loopers-Provider-Key` or configuration) and forwards the sanitized payload to the LLM provider. The Loopers proxy key is never sent upstream.
10. **Outbound Semantic DLP & Mid-Stream SSE Cutoff (Bad Days):** As response tokens stream back, Loopers runs a multi-layer DLP inspection to mask PII, redact internal IPs, or sever the connection if secrets are leaked. If a budget cap is breached mid-generation, the SSE stream is cut off immediately.
11. **Reconciliation & Telemetry:** Reconciles the actual token usage reported by the provider against the estimated lease, releasing unspent funds back to the budget window. Records metrics to Prometheus, spans to OpenTelemetry, and updates the agent's risk profile.

---

## Core Capabilities

### Operational Regime 1: Rogue Agent Defense ("Bad Days" Protection)
* **Multi-Engine Runaway Loop Termination:** Detects and terminates repetitive reasoning and execution loops using three distinct algorithms:
  * *Fingerprint Engine:* Bi-Gram Jaccard similarity detection for repetitive prompts.
  * *Velocity Engine:* Sliding window requests-per-second (RPS) surge limiter.
  * *Stall Engine:* SimHash Hamming distance analysis to catch low-diversity semantic output stalls.
* **Multi-Turn Conversation Drift Detection & Goal Hijacking Protection:** Defends against gradual crescent context divergence and sudden prompt injection goal hijacks across multi-turn sessions. Persists an immutable session anchor ($T_1$) in Redis and calculates weighted containment similarity ($75\%$ anchor grounding + $25\%$ immediate turn continuity) using trigram FNV-1a hashing and English stopword normalization. Proactively terminates diverged requests locally before token consumption.
* **Persistent Agent Risk Identity:** Tracks behavioral history across sessions in Redis with cumulative risk scoring (0–100), automated time-bounded quarantines (e.g., 1 hour), taint flags (`secret_accessed`), and risk decay over time.
* **Outbound Semantic DLP Gate:** Real-time scrubbing of PII (emails, Luhn-validated credit cards, US SSNs, phone numbers), masking of internal network addresses (RFC 1918 IPs, `.internal` hostnames), and quarantine lockouts on secret leaks (AWS, OpenAI, GitHub tokens, private keys) across both JSON and streaming SSE responses.
* **Tool Response Sanitization & Injection Wall:** Scans outbound tool responses, normalizes Unicode, strips zero-width obfuscation characters, and blocks path traversal attempts (`../`) and indirect prompt injection patterns.
* **5-Outcome Policy Action Engine:** Declarative OPA/Rego policy engine supporting five distinct actions: `allow`, `deny`, `escalate` (suspending connection for human approval via Redis Pub/Sub), `quarantine`, and `transform`.
* **Deterministic FSM Tool Sequencing:** Enforces valid state machine transitions on agent tool execution (e.g., requiring `dry_run_command` before allowing destructive `execute_bash` calls).
* **Mid-Stream SSE Cutoff:** Actively parses and counts streaming Server-Sent Events (SSE) tokens chunk-by-chunk, severing the HTTP stream mid-flight the millisecond a budget ceiling is reached.
* **MCP Blast Radius Limiter:** Restricts the maximum number of distinct Model Context Protocol (MCP) servers and tools an agent session can bind to, preventing privilege escalation.

### Operational Regime 2: Inline Context Acceleration ("Normal Days" Optimization)
* **Ghost Token Side-Channel Offloading (`internal/proxy/ghost.go`):** Solves agent context bloat on healthy production traffic. Bulky tool outputs (MCP responses, SQL queries >512 bytes) are encrypted with AES-256-GCM / HKDF and offloaded to local Redis, replacing them with ~80-token Ghost Stubs. Delivers **65% to 75% input token reduction** while dynamically tail-hydrating full raw context on synthesis turns.
* **Deterministic Prefix Canonicalization (`internal/proxy/canonicalizer.go`):** Normalizes tool schema ordering, stabilizes timestamps, and collapses redundant whitespace to guarantee upstream KV prompt cache hits across OpenAI, Anthropic, and OpenRouter for up to **90% prompt cache read discounts**.
* **Smart Fast-Tier Model Routing (`internal/server/router.go`):** Dynamically remaps intermediate navigation and tool verification turns to high-speed, cost-effective models while enforcing warm-cache pinning once multi-turn cache affinity is established.
* **Zero-SDK Session Derivation:** Automatically tracks sessions and trajectories from standard HTTP headers or cryptographic prompt hashing with zero code changes required in agent frameworks.

### Core Architectural Guarantees
* **Atomic Cost Governance:** Multi-window budget limits (minute, hourly, daily, weekly, monthly) enforced atomically via Redis Lua scripts with zero budget over-charge guarantee.
* **Zero-Storage In-Memory Pass-Through:** Provider keys and raw payloads remain strictly in-memory during transit and are never written to disk or persistent databases.
* **Fail-Closed by Design:** If Redis or the firewall experiences an outage, Loopers fails closed, immediately preventing unauthorized spend and unshielded egress.
* **DPoP / Zero-Trust Security Protocol (ZSP):** RFC 9449 Demonstration of Proof-of-Possession for cryptographic agent identity verification, preventing token replay attacks.
* **Shadow Mode:** Operates the firewall in non-blocking observation mode (`server.shadow_mode: true`) to audit policies against live traffic without impacting production workflows.
* **Model Overrides & Header Rewriting:** Transparently rewrite models or remap target endpoints on the fly using `X-Loopers-Model-Override` and `X-Loopers-Model-Map` headers without modifying agent codebases.
* **Offline Trace Verification (`loopers verify`):** Validates past execution trace logs against current policy cards offline, providing verifiable compliance reporting for CI/CD gates.
* **Self-Correcting Error Payloads:** Returns structured, machine-actionable error messages on policy denials and blocks, allowing autonomous agents to understand the rejection reason and self-correct on subsequent turns.

---

## Built-in Security Presets

Loopers includes pre-configured security presets that can be enabled instantly via `--presets` or in `loopers.yaml`:

| Preset | Target & Scope | What It Enforces |
|---|---|---|
| `safety` | LLM Prompts & MCP Tools | Blocks SSNs, credentials, API keys, common prompt injection signatures (`ignore previous instructions`, `dan mode`), and destructive bash commands (`rm -rf`, `sudo`, `chmod 777`, `curl`, `nc -e`). |
| `safety_drift` | Multi-Turn LLM Dialogues | Prevents multi-turn goal hijacking and conversational context divergence. Evaluates prompt similarity against the initial session anchor ($T_1$) and blocks turns that deviate from the authorized session objective. |
| `pci` | LLM Prompts & Payloads | Enforces PCI-DSS input sanitization by blocking 16-digit credit card numbers, CVV/CVC codes, and SQL injection signatures (`UNION SELECT`, `DROP TABLE`, `OR 1=1`). |
| `mcp_sandbox` | MCP Tool Invocations | Prevents path traversal (`../`) in filesystem arguments and enforces stateful tool chaining: requires `dry_run_command` to be executed within the last 2 steps before running `execute_bash`. |
| `zero_trust` | Agent Identity & Risk | Denies LLM and MCP tool access to any agent with a persistent risk score above 75. Automatically escalates `send_email` tool calls to human review if the agent holds a `secret_accessed` taint flag. |

Enable presets on startup:
```bash
loopers serve --presets safety,safety_drift,pci,mcp_sandbox
```

---

## Quickstart

### Prerequisites
- **Go 1.26.6+** or **Docker**
- **Redis 7+** (required for atomic state and budget governance)

### Step 1: Install Loopers

```bash
# Via Homebrew (macOS / Linux)
brew install cursed-me/tap/loopers

# Via Docker
docker pull ghcr.io/try-loopers/loopers:latest

# Build from Source
git clone https://github.com/try-loopers/loopers-oss.git
cd loopers-oss
go install ./cmd/loopers
```

### Step 2: Initialize Configuration & Start Redis

```bash
# Start local Redis instance
docker run -d --name loopers-redis -p 6379:6379 redis:7-alpine

# Initialize Loopers configuration wizard (creates loopers.yaml and docker-compose.yml)
loopers init

# Run system diagnostic checks
loopers doctor
```
*Note: For native installs, ensure the `pricing.yaml` file from the repository root is present in your working directory, as Loopers requires it at startup.*

### Step 3: Create a Proxy Key

```bash
loopers keys create --name dev-agent --provider openai
```
*Note: This generates a raw proxy key (e.g. `lp-a1b2c3d4...`) and its SHA-256 hash. Save the raw key immediately.*

### Step 4: Configure Budget Limits

```bash
# Set hourly and daily spending limits on the key hash
loopers budget set <KEY_HASH> --daily 5.00 --hourly 1.00

# Verify current budget allocations
loopers budget status <KEY_HASH>
```

### Step 5: Start the Firewall

```bash
# Run in development mode
SERVER_INSECURE_DEV=true loopers serve --presets safety
```

<details>
<summary><b>Alternative: Development with Docker Compose (Local Dev)</b></summary>

If you want to run Redis and the Loopers firewall together using Docker Compose:
```bash
# 1. Start Redis cache dependency
docker-compose up -d redis

# 2. Run the Loopers firewall locally
SERVER_INSECURE_DEV=true loopers serve --presets safety
```
</details>

> [!WARNING]
> `SERVER_INSECURE_DEV=true` disables TLS enforcement for local testing. In production environments, configure valid TLS certificates in `loopers.yaml`.

### Step 6: Route Your Agent Traffic

#### Option A: Transparent CLI Wrapper (Zero Code Changes)
```bash
export LOOPERS_PROXY_KEY="lp-your-proxy-key"
export OPENAI_API_KEY="sk-your-actual-provider-key"

# Wrap any agent CLI tool directly. Auto-detection supports:
# aider, openhands, pi, claude, codex, opencode, gemini, dsh, deepseek-harness, deepseek.
# Other tools require setting LOOPERS_PROVIDER explicitly.
loopers exec -- aider
```

#### Option B: Base URL Redirection
Point your agent client directly to the Loopers proxy endpoint:
```bash
export OPENAI_BASE_URL="http://localhost:8080/lp-your-proxy-key/openai/v1"
export OPENAI_API_KEY="sk-your-actual-provider-key"

aider
```

#### Option C: Programmatic Python Integration
```python
from loopers_client import LoopersOpenAI

client = LoopersOpenAI(
    loopers_url="http://localhost:8080",
    loopers_key="lp-your-proxy-key",
    provider_key="sk-your-actual-provider-key",
    session_budget=2.00,  # Cap session spend at $2.00
    max_steps=20          # Terminate if session exceeds 20 turns
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Analyze system logs and report findings."}]
)
print(response.choices[0].message.content)
```

---

## CLI Reference

| Command | Description |
|---|---|
| `loopers serve` | Starts the firewall proxy server and admin metrics listener. Accepts `--presets` and `--config`. |
| `loopers init` | Interactive terminal UI (TUI) wizard to generate a production-ready `loopers.yaml`. |
| `loopers doctor` | Verifies Redis connectivity, pricing configuration, policy directories, and dependencies. |
| `loopers exec -- <cmd>` | Wraps and executes agent CLI binaries with transparent proxy injection and auto-detected providers. |
| `loopers verify -t <trace>` | Audits historical agent execution traces offline against security policies. Supports `--format json` for CI/CD. |
| `loopers keys create` | Provisions a new agent proxy key (`lp-...`) with associated identity metadata and provider bindings. |
| `loopers keys list` | Lists all registered agent proxy keys, provider assignments, and active statuses. |
| `loopers keys revoke <hash>` | Instantly revokes a proxy key, preventing all subsequent inbound agent traffic. |
| `loopers budget set <hash>` | Sets minute, hourly, daily, weekly, or monthly spending limits for an agent key. |
| `loopers budget status <hash>`| Displays real-time window consumption, remaining balance, and lease allocations. |

---

## Observability & Telemetry

### Prometheus Metrics
Loopers runs an isolated admin server on `:9090` exposing Prometheus metrics at `/metrics`:

| Metric Name | Type | Description |
|---|---|---|
| `loopers_requests_total` | Counter | Total AI requests processed partitioned by `provider`, `model`, and `status`. |
| `loopers_budget_blocks_total` | Counter | Total requests blocked by budget governance per `provider` and `window`. |
| `loopers_loop_blocks_total` | Counter | Total requests halted by loop detection engines (`fingerprint`, `velocity`, `stall`). |
| `loopers_spend_usd_total` | Counter | Cumulative USD spend tracked per `provider` and `key_hash`. |
| `loopers_dlp_redactions_total` | Counter | Total outbound LLM completions redacted by semantic DLP. |
| `loopers_dlp_quarantines_total` | Counter | Total agent keys locked out due to secret exfiltration detection. |
| `loopers_policy_escalations_total` | Counter | Total policy decisions suspended and escalated for human approval. |
| `loopers_mcp_tool_calls_total` | Counter | Total MCP tool invocations tracked per `tool_name` and `status`. |

*A pre-configured [Grafana Dashboard](./grafana/) is provided in the repository for turnkey deployment.*

### OpenTelemetry Distributed Tracing
Loopers exports standard OpenTelemetry spans via OTLP (gRPC/HTTP) adhering to `gen_ai.*` semantic conventions. Configure your collector in `loopers.yaml`:
```yaml
otel:
  enabled: true
  endpoint: "localhost:4317"
  protocol: "grpc"
  sampling_rate: 1.0
```
*Note: All security enforcement actions (blocks, quarantines, escalations) are traced at 100% fidelity regardless of the configured sampling rate.*

### Real-Time Webhook Alerts
Configure HMAC-SHA256 signed webhooks to alert external incident systems on security anomalies and budget thresholds:
```yaml
alerting:
  webhook_url: "https://ops.example.com/alerts"
  webhook_secret: "env:LOOPERS_WEBHOOK_SECRET"
  thresholds:
    - percent: 50
      message: "Agent budget at 50% capacity"
    - percent: 90
      message: "Agent budget near exhaustion — cutoff imminent"
```

---

## Documentation Index

Explore our comprehensive guides and architectural documentation:

* **Budget Governance:** [Sliding Budget Windows](./Documentation/docs/concepts/budget-windows.md) | [Per-Session Budgets](./Documentation/docs/concepts/session-budgets.md)
* **Agent Circuit Breakers:** [Runaway Loop Detection Guide](./Documentation/docs/concepts/agent-loop-detection.md)
* **Policy Engine & FSM:** [OPA Rego & YAML Policy Cards](./Documentation/docs/guides/policy-engine.md)
* **Tool Sandboxing:** [Model Context Protocol (MCP) Setup](./Documentation/docs/guides/mcp-setup.md)
* **Zero-Code Integration:** [Agent CLI Wrappers & Environments](./Documentation/docs/guides/agent-cli-integrations.md)
* **Monitoring:** [Grafana Dashboards & Prometheus Setup](./Documentation/docs/guides/monitoring-grafana.md)
* **CI/CD Compliance:** [Offline Trace Verification Guide](./Documentation/docs/guides/trace-verification.md)

---

## Contributing

We welcome contributions from the open-source community!

* **Bug Reports & Feature Requests:** Submit an issue on [GitHub Issues](https://github.com/try-loopers/loopers-oss/issues).
* **Community Discussions:** Join conversations on [GitHub Discussions](https://github.com/try-loopers/loopers-oss/discussions).
* **Development Workflow:** Read our [Contributing Guide](./CONTRIBUTING.md) to learn about code standards, running test suites, and opening pull requests.

---

## License

Loopers is distributed under the [MIT License](./LICENSE).
