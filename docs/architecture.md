# Architecture Overview

Loopers is engineered as a high-performance **Dual-Regime AI Firewall**. It operates as a transparent reverse proxy between autonomous agent applications and upstream LLM providers, providing rigorous runtime security boundaries when agents misbehave ("Bad Days") alongside inline context compression and wire acceleration on healthy everyday traffic ("Normal Days").

```
┌────────────────────────────────────────────────────────────────────────┐
│                             Agent Runtime                              │
│       (Aider / Claude Code / OpenHands / LangChain / Python SDK)       │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ HTTP / SSE (e.g., OPENAI_BASE_URL)
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│                      Loopers AI Firewall (:8080)                       │
│                                                                        │
│ ┌────────────────────────────────────────────────────────────────────┐ │
│ │          REGIME 1: ROGUE AGENT DEFENSE & THREAT MITIGATION         │ │
│ │                     ("Bad Days" Protection)                        │ │
│ │  [1. Key Auth & DPoP] ──► [2. Risk Score & Quarantine Gate]        │ │
│ │  [3. Syntactic Normalizer] ──► [4. Multi-Turn Drift Containment]   │ │
│ │  [5. Pre-Call Budget Lease] ──► [6. OPA / YAML Policy Engine & FSM]│ │
│ │  [7. Multi-Engine Loop Detector] ──► [8. Outbound Semantic DLP]    │ │
│ └────────────────────────────────────────────────────────────────────┘ │
│                                   │                                    │
│ ┌────────────────────────────────────────────────────────────────────┐ │
│ │         REGIME 2: INLINE CONTEXT ACCELERATION & WIRE FINOPS        │ │
│ │                    ("Normal Days" Optimization)                    │ │
│ │  [A. Zero-SDK Session Derivation]                                  │ │
│ │  [B. Ghost Token Side-Channel Offload (Redis AES-256-GCM / HKDF)]  │ │
│ │  [C. Dynamic Tail-Hydration (Frontier Synthesis vs Fast-Tier)]     │ │
│ │  [D. Deterministic Prefix Canonicalizer (KV Cache Maximization)]   │ │
│ │  [E. Smart Fast-Tier Model Routing & Warm Cache Pinning]           │ │
│ └────────────────────────────────────────────────────────────────────┘ │
│                                   │                                    │
│  Admin Server (:9090): /metrics (Prometheus) | /health                 │
│  Telemetry: OpenTelemetry Traces (OTLP gRPC/HTTP) | HMAC Webhooks      │
└───────────────────┬──────────────────────────────────┬─────────────────┘
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

---

## 1. System Components

1. **Agent Application**: Any agentic framework (LangChain, CrewAI, AutoGen, Claude Code, Aider, custom scripts) interacting with Loopers via standard OpenAI, Anthropic, or Gemini client SDKs by pointing the base URL to Loopers.
2. **Loopers AI Firewall**: Core engine written in Go. Operates stateless regarding provider keys, adding minimal sub-2ms network latency while running dual-regime enforcement and token canonicalization.
3. **Redis 7+ State Store**: Distributed memory backend executing atomic Lua leases, encrypted ghost token blob storage (AES-256-GCM), SimHash loop fingerprints, and persistent agent risk identities.
4. **Upstream LLM Provider**: The target model provider (OpenRouter, DeepSeek, OpenAI, Anthropic, AWS Bedrock, Google Gemini).

---

## 2. Dual-Regime Firewall Operation

### Regime 1: Rogue Agent Defense ("Bad Days" Protection)
When agents enter recursive reasoning loops, cascade failures, or suffer from indirect prompt injection, Loopers acts as an active circuit breaker:
- **Layer 1: Key Auth & DPoP RFC 9449:** Verifies Loopers proxy keys (`lp-...`) via HMAC-SHA256 and validates cryptographic Proof-of-Possession signatures.
- **Layer 2: Persistent Behavioral Risk Profile:** Cumulative agent risk scoring (0–100) across sessions in Redis. Triggers automatic 1-hour quarantine lockouts at score >75 and permanent blocks at >90.
- **Layer 3: Syntactic Normalization (`internal/syntactic`):** 5-stage de-obfuscation resolving Unicode TR39 homoglyphs, stripping zero-width runes, and decoding nested Base64/URL entities.
- **Layer 4: Pre-Call Atomic Budget Leases:** Multi-window quota reservation (minute, hourly, daily, weekly, monthly) enforced atomically with 0% over-charge guarantee.
- **Layer 5: OPA & YAML Policy Engine with FSM:** Enforces deterministic state machine transitions on tools (e.g. requiring `dry_run` before destructive shell commands) and multi-turn objective drift containment against initial session anchors ($T_1$).
- **Layer 6: Multi-Engine Loop Breaker:** Triggers circuit-breakers on repeating trajectories via bi-gram Jaccard similarity, sliding-window RPS velocity surges, and SimHash Hamming distance stalls.
- **Layer 7: Outbound Semantic DLP & Mid-Stream SSE Cutoff:** Real-time scrubbing of PII, internal IP redaction, secret quarantine, and chunk-by-chunk token budget enforcement that severs runaway streams mid-generation.

### Regime 2: Inline Context Acceleration ("Normal Days" Optimization)
Even when agents are completely healthy and operating as expected, accumulating multi-turn tool dumps creates exponential token bills. Loopers optimizes this healthy traffic inline:
- **Ghost Token Side-Channel Offloading (`internal/proxy/ghost.go`):** Offloads bulky tool outputs (MCP responses, SQL payloads, logs > 512 bytes) into local Redis with AES-256-GCM / HKDF encryption, replacing them in the prompt context with lightweight, schema-compliant Ghost Stubs (~80 tokens). Yields **65%–75% input token reduction**.
- **Dynamic Prompt Tail-Hydration (`HydratePromptTail`):** Automatically detects synthesis turns requiring frontier reasoning (`!isSimpleTurn`), appending full raw payloads via `<loopers_hydrated_context>` for final reports while keeping intermediate navigation turns ultra-compact.
- **Deterministic Prefix Canonicalization (`internal/proxy/canonicalizer.go`):** Re-orders tool schema definitions, normalizes whitespace, and stabilizes timestamps to guarantee upstream KV prompt cache hits (yielding 90% cache read discounts).
- **Smart Fast-Tier Routing (`internal/server/router.go`):** Routes intermediate verification turns to fast models (e.g., `deepseek-v4-pro` -> `deepseek-v4.1-flash-1017`) on cold cache, while preserving model pinning on warm cache to exploit provider prompt cache read discounts.
- **Zero-SDK Session Derivation:** Automatically tracks sessions using standard headers (`X-Loopers-Session-ID`, `X-Session-ID`, `X-Thread-ID`) or cryptographic hashing of prompt prefixes without requiring agent code changes.

---

## 3. Key Security & Operational Guarantees

- **Zero-Storage Pass-Through:** Provider API keys and unencrypted payloads reside strictly in-memory during transit and are never written to disk or databases.
- **Fail-Closed by Design:** If Redis or the firewall experiences an outage, Loopers fails closed, immediately preventing unauthorized spend and unshielded egress.
- **Atomic Concurrency Control:** Budget checks execute via Lua scripts, guaranteeing zero over-charge even under thousands of parallel concurrent agent threads.
