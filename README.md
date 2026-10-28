<p align="center">
  <img src="./docs/cover.png" alt="Loopers" width="800"/>
</p>

# Loopers

### The AI Firewall for the Agentic Era

> **The Dual-Regime AI Firewall: Cut normal LLM cloud spend by up to 68% with Ghost Tokens, and stop runaway agent loops, data exfiltration, and budget spikes before they bill. Zero SDK required.**

<p align="left">
  <img src="https://img.shields.io/badge/license-MIT-black.svg?style=for-the-badge" alt="License" />
  <img src="https://img.shields.io/badge/go-1.26.6%2B-black.svg?style=for-the-badge" alt="Go Version" />
  <img src="https://img.shields.io/badge/models-500%2B%20Models%20%C2%B7%2015%20Providers-black.svg?style=for-the-badge" alt="Models and Providers" />
  <img src="https://img.shields.io/badge/architecture-Fail--Closed-black.svg?style=for-the-badge" alt="Fail Closed Architecture" />
  <img src="https://img.shields.io/badge/budget%20leakage-0%25%20Over--Charge%20by%20Design-black.svg?style=for-the-badge" alt="Zero Over-Charge Guarantee" />
  <img src="https://img.shields.io/badge/redis-7%2B%20Required-black.svg?style=for-the-badge" alt="Redis 7+ Required" />
</p>

### Proven Dual-Regime Value at a Glance

| Everyday Normal Operations ("Normal Days") | Rogue Threat Defense ("Bad Days") |
| :--- | :--- |
| **65% to 76% Input Token Reduction** via Ghost Token offloading | **100% Real-Time Loop Termination** (Fingerprint, Velocity, Stall) |
| **40,000+ Schema Tokens Stripped** via inline MCP Minifier | **0% Budget Over-Charge Guarantee** via atomic pre-call Redis leases |
| **Up to 68.35% Net LLM Bill Savings** on enterprise multi-turn workflows | **Sub-Millisecond DLP Redaction** (scrubs PII, secrets, internal IPs) |
| **94.6% Discovery Cost Drop** via smart cold-turn fast tier routing | **Deterministic Policy & FSM** (OWASP Top 10, NIST, EU AI Act presets) |
| **3.8 ms Cached Tool Execution** at **$0.00** upstream API cost | **Fail-Closed Security** (blocks unshielded egress on connection drop) |

---

## The Problem: The Two Financial & Operational Crises of Autonomous Agents

Every enterprise running autonomous agents faces two distinct operational regimes, and existing tools fail at both:

### Regime 1: Bad Days - Runaway Loops, Data Exfiltration & Silent Budget Exhaustion
When autonomous agents fail, they fail silently, recursively, and catastrophically:
* **Runaway Loops & Reasoning Stalls**: A single stuck retry loop or recursive reasoning stall can burn hundreds of dollars in API credits within minutes. Upstream provider dashboards only provide post-hoc alerts hours after the bill has hit your card.
* **Tool Poisoning & Data Exfiltration**: Untrusted web scraping or poisoned MCP tool responses turn agents into exfiltration vectors, leaking internal credentials, secrets, and customer PII back to untrusted endpoints.
* **Prompt-Based Safety Fails**: Relying on system prompts for security boundaries fails under prompt injections, jailbreaks, or long-context drift.

### Regime 2: Normal Days - Silent Context Bloat & Crushing Cloud Bills
Even when autonomous agents execute tasks correctly with zero errors, multi-turn tool calling is mathematically broken:
* **70% to 80% of context is repetitive baggage**: Agents repeatedly drag massive historical tool responses (bulky MCP JSON schemas, database query dumps, file reads) into every subsequent turn. Teams repeatedly pay to transmit the exact same megabytes back and forth.
* **Frontier Model Over-Provisioning**: Simple intermediate exploration steps (triage, reading docs, formatting) are needlessly executed on expensive frontier models ($0.20 to $5.00/1M tokens) instead of fast-tier models.
* **The Impact**: Production agent deployments burn through thousands of dollars on completely normal, error-free days.

Loopers sits at the network wire as an active, inline proxy that solves **both regimes simultaneously**: cutting everyday cloud bills by up to **68.35%** on normal days, while enforcing strict **0% over-charge** circuit breakers and policy governance on bad days.

### Origins & Founder Story

Loopers was founded and is actively developed by solo founder **[Varad Khoriya](https://github.com/CURSED-ME)**.

The project was born when an autonomous test agent got stuck in a recursive loop during local development, burning through unexpected API credits. When analyzing the post-mortem, it became obvious that the ecosystem had two massive blind spots: major AI platforms only offer passive post-hoc billing dashboards rather than real-time circuit breakers, and healthy multi-turn agents are forced to resend massive, uncompressed context baggage on every ordinary turn.

I built Loopers to establish an open-source, bare-metal AI firewall standard: delivering verifiable 68%+ FinOps savings on normal days and active circuit-breaker protection when agents go rogue.

Have questions, suggestions, or need help integrating? Join the community on [GitHub Discussions](https://github.com/try-loopers/loopers-oss/discussions) or submit an issue on [GitHub Issues](https://github.com/try-loopers/loopers-oss/issues).

> **AI Agents & LLMs:** See [AGENT_README.md](./AGENT_README.md) for dense machine-readable context and configuration schemas.

---

## What Loopers Is and Is Not

Loopers is a **stateful, runtime security firewall** for autonomous AI agents.  
It is **not** an AI gateway, passive API wrapper or multi-provider aggregator, prompt management platform, or agent orchestration framework.

### The Firewall Difference

| Capability | AI Gateways (LiteLLM, PortKey, OpenRouter) | Loopers AI Firewall |
|---|---|---|
| **Primary Focus** | Traffic routing, model aggregation, latency | **Dual-Regime:** Rogue threat defense ("Bad Days") + Inline wire acceleration ("Normal Days") |
| **Budget Enforcement** | Post-request tracking or passive webhook alerts | Pre-call atomic leases with 0% over-charge by design |
| **Streaming Enforcement** | Passes stream chunks transparently | Active chunk-by-chunk SSE inspection; severs connections mid-stream |
| **Context Compression (Ghost Tokens)** | None (raw payloads sent to LLM) | Ephemeral AES-256-GCM side-channel offload; **65% to 75% input token reduction** |
| **Prefix Canonicalization** | None | Deterministic tool schema sorting & whitespace normalization for **90% KV cache hits** |
| **Smart Fast-Tier Routing** | Static fallback on error | Dynamic routing of intermediate turns to fast-tier models with warm-cache pinning |
| **Syntactic Normalization** | None | 5-stage de-obfuscation (Unicode homoglyphs, invisible runes, base64 decoding, token collapse) |
| **Outbound DLP Gate** | None | Real-time scanning & redaction of PII, secrets, and private IPs |
| **Agent Behavioral Risk** | Stateless per request | Persistent cross-session risk scoring (0 to 100) & automated quarantine |
| **Runaway Loop Breaker** | None | Real-time 3-engine detection (Fingerprint, Velocity, Stall) |
| **Multi-Turn Drift Protection** | Stateless per request | Active containment scoring against initial session anchor ($T_1$) |
| **Compliance Presets** | None | Out-of-the-box zero-dependency policies for OWASP LLM Top 10, NIST AI RMF 1.0, and EU AI Act |
| **Tool Blast Radius Scoring** | None | Deterministic risk scoring (0 to 100) & tier classification on tool verbs, targets, wildcards, and egress |
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

1. **Key Authentication & DPoP Validation:** Authenticates the proxy key (`lp-...`) via Redis HMAC-SHA256 lookup and verifies RFC 9449 Demonstration of Proof-of-Possession (DPoP) JWT headers against public key thumbprints (`jkt`) with anti-replay tracking.
2. **Quarantine & Behavioral Risk Gate:** Evaluates the agent's persistent behavioral risk score in Redis. Rejects traffic if quarantined (threshold: 75) or permanently drops the key if risk exceeds the permanent block ceiling (score > 90).
3. **Sliding-Window Rate Limiting & Concurrency Gate:** Checks per-key sliding window request limits. Under rate-limit contention, live HTTP requests can suspend and escalate for human approval via Redis Pub/Sub.
4. **Layer 3 Syntactic Normalization & Payload Parsing:** Parses the inbound payload and executes the 5-stage de-obfuscation pipeline (`internal/syntactic`): normalizes Unicode homoglyphs, strips zero-width/bidi runes, decodes recursive Base64/URL percent escapes, and collapses delimited tokens (`i.g.n.o.r.e`).
5. **Deterministic Policy Engine & FSM Sequencing:** Evaluates Open Policy Agent (OPA) Rego rules and declarative YAML Policy Cards against canonical inputs. Validates tool blast radius scoring, computes multi-turn conversation drift ($T_1$ session anchor), and gates tool sequences via finite state machines. Supports 5 deterministic actions: `allow`, `deny`, `escalate`, `quarantine`, and `transform`.
6. **Smart Fast-Tier Routing & Synthesis-Gated Tail-Hydration:** Evaluates multi-turn cache affinity. On cold discovery turns, remaps queries to fast-tier models (saving up to 94.6% on discovery). On synthesis turns, securely decrypts and appends hydrated context to the prompt tail without corrupting upstream KV cache prefixes.
7. **Deterministic Prefix Canonicalization:** Normalizes tool schema ordering, stabilizes system anchors, and collapses JSON whitespace to guarantee upstream KV prompt cache hits across Anthropic, OpenAI, and OpenRouter for up to 90% prompt cache read discounts.
8. **Pre-Call Atomic Budget Lease:** Computes the maximum estimated cost using model rates from `pricing.yaml` and atomically reserves quota in Redis via Lua scripts across minute, hourly, daily, weekly, and monthly sliding windows with 0% over-charge guarantee.
9. **Multi-Engine Runaway Loop Detection:** Evaluates session trajectory against history using Bi-Gram Jaccard similarity (Fingerprint), sliding-window velocity limiter (Velocity), and SimHash Hamming distance (Stall).
10. **Upstream Request Forwarding:** Strips the proxy key, injects the real provider key (from `X-Loopers-Provider-Key` or vault config), and forwards the sanitized request to the upstream LLM provider.
11. **Outbound Semantic DLP & Mid-Stream SSE Cutoff:** As tokens stream back via Server-Sent Events (SSE), runs chunk-by-chunk DLP to redact PII, internal IPs, and secrets. If a budget cap is reached mid-stream, severs the TCP connection immediately.
12. **Atomic Settlement & Telemetry:** Reconciles actual token usage from provider completion headers against the estimated lease, atomically releasing unspent funds back to Redis. Emits Prometheus metrics (`/metrics`) and W3C OpenTelemetry distributed traces.

---

## Core Capabilities

### Operational Regime 1: Rogue Agent Defense ("Bad Days" Protection)
* **Multi-Engine Runaway Loop Termination:** Detects and terminates repetitive reasoning and execution loops using three distinct algorithms:
  * *Fingerprint Engine:* Bi-Gram Jaccard similarity detection for repetitive prompts.
  * *Velocity Engine:* Sliding window requests-per-second (RPS) surge limiter.
  * *Stall Engine:* SimHash Hamming distance analysis to catch low-diversity semantic output stalls.
* **Multi-Turn Conversation Drift Detection & Goal Hijacking Protection:** Defends against gradual crescent context divergence and sudden prompt injection goal hijacks across multi-turn sessions. Persists an immutable session anchor ($T_1$) in Redis and calculates weighted containment similarity ($75\%$ anchor grounding + $25\%$ immediate turn continuity) using trigram FNV-1a hashing and English stopword normalization. Proactively terminates diverged requests locally before token consumption.
* **Persistent Agent Risk Identity:** Tracks behavioral history across sessions in Redis with cumulative risk scoring (0 to 100), automated time-bounded quarantines (e.g., 1 hour), taint flags (`secret_accessed`), and risk decay over time.
* **Outbound Semantic DLP Gate:** Real-time scrubbing of PII (emails, Luhn-validated credit cards, US SSNs, phone numbers), masking of internal network addresses (RFC 1918 IPs, `.internal` hostnames), and quarantine lockouts on secret leaks (AWS, OpenAI, GitHub tokens, private keys) across both JSON and streaming SSE responses.
* **Tool Response Sanitization & Injection Wall:** Scans outbound tool responses, normalizes Unicode, strips zero-width obfuscation characters, and blocks path traversal attempts (`../`) and indirect prompt injection patterns.
* **5-Outcome Policy Action Engine:** Declarative OPA/Rego policy engine supporting five distinct actions: `allow`, `deny`, `escalate` (suspending connection for human approval via Redis Pub/Sub), `quarantine`, and `transform`.
* **Deterministic FSM Tool Sequencing:** Enforces valid state machine transitions on agent tool execution (e.g., requiring `dry_run_command` before allowing destructive `execute_bash` calls).
* **Mid-Stream SSE Cutoff:** Actively parses and counts streaming Server-Sent Events (SSE) tokens chunk-by-chunk, severing the HTTP stream mid-flight the millisecond a budget ceiling is reached.
* **MCP Blast Radius Limiter:** Restricts the maximum number of distinct Model Context Protocol (MCP) servers and tools an agent session can bind to, preventing privilege escalation.

### Operational Regime 2: Inline Context Acceleration ("Normal Days" Optimization)
* **Ghost Token Side-Channel Offloading (`internal/proxy/ghost.go`):** Solves agent context bloat on healthy production traffic. Bulky tool outputs (MCP responses, SQL queries > configurable threshold from 512 B to 8 KB) are encrypted with AES-256-GCM / HKDF and offloaded to local Redis, replacing them with ~80-token Ghost Stubs. Delivers **65% to 75.8% input token reduction** while dynamically tail-hydrating full raw context on synthesis turns.
* **Deterministic Prefix Canonicalization (`internal/proxy/canonicalizer.go`):** Normalizes tool schema ordering, stabilizes timestamps, and collapses redundant whitespace to guarantee upstream KV prompt cache hits across OpenAI, Anthropic, and OpenRouter for up to **90% prompt cache read discounts** (77.01% empirical KV hit rate).
* **Smart Fast-Tier Model Routing (`internal/server/router.go`):** Dynamically remaps cold discovery and verification turns to high-speed models (delivering an immediate **94.6% cost reduction** on cold discovery turns) while enforcing warm-cache pinning once multi-turn cache affinity is established.
* **MCP Schema Minification (`cmd/loopers/mcp.go` & `internal/mcp/sanitizer.go`):** Intercepts Model Context Protocol `tools/list` handshakes across both HTTP and local stdio processes, trimming redundant descriptions, JSON Schema meta-tags, and documentation bloat to strip tens of thousands of prompt discovery tokens on every turn while preserving 100% parameter validity.
* **Read-Only Tool Memoization (`internal/mcp/handler.go`):** Intercepts repeat read-only MCP tool invocations and serves them from Redis in **3.8 ms** with zero upstream latency and **$0.00** API charges.
* **Empirical Validation:** Benchmarked across 60 multi-turn production agent workloads, achieving a net **68.35% reduction in total enterprise cloud API spend** across combined context compression, KV cache maximization, and fast-tier routing.
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
| `owasp_llm_top10` | Full Agent Pipeline | Comprehensive out-of-the-box coverage for the OWASP Top 10 for LLMs (LLM01 Prompt Injection, LLM02 Sensitive Information Disclosure, LLM10 Unbounded Consumption). |
| `nist_ai_rmf` | Risk Governance | Maps runtime guardrails directly to the NIST AI Risk Management Framework 1.0 (MAP, MEASURE, MANAGE functions). |
| `eu_ai_act` | Regulatory Compliance | Enforces EU AI Act Article 12 (deterministic record-keeping) and Article 14 (human oversight, emergency circuit breakers) runtime requirements. |

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
brew install try-loopers/tap/loopers

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

#### Option D: Transparent Local MCP Stdio Wrapper (`loopers mcp wrap`)
For local autonomous agents (e.g. ZeroClaw, Claude Desktop, Cursor) using Model Context Protocol tools via standard I/O pipes (like `npx`, `python`, `node`), wrap the child command directly without modifying your agent codebase:

```toml
# Example: zeroclaw config.toml
[[mcp.servers]]
name = "notion"
transport = "stdio"
command = "loopers"
args = ["mcp", "wrap", "--server", "notion", "--key", "lp-your-proxy-key", "--", "npx.cmd", "-y", "mcp-remote", "https://mcp.notion.com/mcp"]
```

```json
// Example: claude_desktop_config.json
{
  "mcpServers": {
    "fetch": {
      "command": "loopers",
      "args": ["mcp", "wrap", "--server", "fetch", "--key", "lp-your-proxy-key", "--", "python", "-m", "mcp_server_fetch"]
    }
  }
}
```

Loopers sits directly on the OS pipe:
- Compresses `tools/list` JSON schemas on the fly (stripping 40,000+ discovery tokens).
- Encrypts tool outputs >4KB into Redis and injects ~80-token Ghost Stubs.
- Streams real-time FinOps telemetry directly into your live terminal dashboard.

---

## CLI Reference

| Command | Description |
|---|---|
| `loopers` | Interactive terminal UI (TUI) dashboard with live auto-refreshing FinOps Scorecard (Spend, Counterfactual, Net Savings, Ghost Tokens, Schema Minifier) and timeframe hotkeys. |
| `loopers mcp wrap` | Transparently wraps local subprocess MCP tools over stdio to apply Schema Minification and Ghost Token offloading. |
| `loopers mcp bridge` | Bridges local standard I/O (stdin/stdout) JSON-RPC agents to the Loopers HTTP firewall proxy. |
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
| `loopers_savings_usd_total` | Counter | Total USD saved via prompt caching and token optimization per `provider` and `key_hash`. |
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
      message: "Agent budget near exhaustion: cutoff imminent"
```

---

## Documentation Index

Explore our comprehensive guides and architectural documentation:

* **FinOps & Budget Controls:** [Context Acceleration & Ghost Tokens](./Documentation/docs/concepts/context-acceleration.md) | [Sliding Budget Windows](./Documentation/docs/concepts/budget-windows.md) | [Per-Session Budgets](./Documentation/docs/concepts/session-budgets.md)
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
