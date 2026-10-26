---
id: context-acceleration
title: Inline Context Acceleration & Ghost Tokens
sidebar_label: Context Acceleration
description: Deep dive into Regime 2 optimization - Ghost Tokens, dynamic tail-hydration, deterministic prefix canonicalization, and fast-tier routing.
---

# Inline Context Acceleration & Ghost Tokens

While **Regime 1 (Rogue Agent Defense)** acts as an active circuit breaker when agents malfunction or loop, **Regime 2 (Inline Context Acceleration & Wire Optimization)** operates continuously on healthy everyday traffic.

In multi-turn autonomous agent swarms (e.g., coding agents, research assistants, data pipelines), agents repeatedly pass cumulative message histories containing massive tool outputs, file contents, and API responses. Even when the agent behaves correctly, this causes quadratic token bill inflation.

Loopers solves this at the wire layer, cutting input token spend by **65% to 75%** with zero loss in model reasoning or output quality.

---

## The Quadratic Context Problem

Autonomous agents interact with tools using a multi-turn ReAct or plan-and-solve loop:

1. **Turn 1**: Agent requests file read or database query.
2. **Turn 2**: Tool returns 15,000 tokens of raw data. Agent reads it and plans next action.
3. **Turn 3**: Agent requests a secondary lookup. Prompt now carries Turn 2's 15,000 tokens.
4. **Turn 4**: Prompt now carries 30,000 tokens.
5. **Turn 10**: The cumulative prompt carries over 100,000 tokens of mostly historical, already-processed tool outputs.

By turn 10, **over 80% of the tokens billed are redundant historical tool results**. This leads to:
* Exponential API cost inflation.
* Slower time-to-first-token (TTFT) and increased network latency.
* "Lost in the middle" attention degradation in LLMs.

---

## 1. Ghost Tokens: Side-Channel KV-Cache Offload

Loopers intercepts inbound agent payloads in `internal/proxy/ghost.go` and offloads large tool execution payloads into an ephemeral side-channel:

```
┌────────────────────────────────────────────────────────────────────────┐
│ Agent Client                                                           │
│ [Turn 3]: Message history with 15KB file read from Turn 2              │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Inbound HTTP Request
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ Loopers AI Firewall (internal/proxy/ghost.go)                          │
│ 1. Scan tool role messages (`role: "tool"` or `role: "function"`)      │
│ 2. Payload size > threshold_bytes (512B)?                              │
│ 3. Encrypt payload with AES-256-GCM (HKDF-derived secret)              │
│ 4. Offload to Redis: SET ghost:<sha256> <encrypted_blob> EX 3600       │
│ 5. Replace wire content with 80-token Ghost Stub                       │
└───────────────────┬──────────────────────────────────┬─────────────────┘
                    │                                  │
                    ▼ Raw Payload (AES-256-GCM)        ▼ Compressed Prompt (~80 tokens)
          ┌───────────────────┐              ┌───────────────────┐
          │      Redis        │              │ Upstream Provider │
          │  ghost:<sha256>   │              │ (Anthropic/OpenAI)│
          └───────────────────┘              └───────────────────┘
```

### The Ghost Stub

The raw 15,000-token tool response is replaced on the wire by a compact, schema-compliant JSON stub:

```json
{
  "status": "success",
  "summary": "[Loopers Ghost Token: payload offloaded to side-channel cache]",
  "ghost_ref": "lp_ghost_7f83b1657ff1fc53b92dc18148a1d65dfc2d4b1fa3d677284addd200126d9069",
  "bytes_saved": 15360
}
```

The LLM still understands that the tool execution succeeded and can reference the `ghost_ref`, but the prompt token payload drops from 15,000 tokens to under 80 tokens.

---

## 2. Dynamic Tail-Hydration (Zero Quality Loss)

A common concern with token pruning is: *If the model cannot see the raw tool output on later turns, how can it generate an accurate final answer or report?*

Loopers solves this with **Dynamic Prompt Tail-Hydration** (`HydratePromptTail`):

1. **Turn Classification**: Loopers inspects the agent's message structure and intent. Intermediate turns (e.g. navigation, verification checks, or issuing subsequent tool calls) execute with compressed Ghost Stubs.
2. **Frontier Synthesis Detection**: When the agent reaches a synthesis or final completion turn (`!isSimpleTurn`), Loopers recognizes that full context is required.
3. **Just-in-Time Decryption**: Loopers fetches the referenced ghost blobs from Redis, decrypts them in volatile memory, and injects them into an explicit context container at the tail of the prompt:

```markdown
<loopers_hydrated_context>
[Turn 2 tool_call: read_file]
... full raw decrypted file contents (15,000 tokens) ...
</loopers_hydrated_context>
```

4. **Zero Quality Regressions**: The frontier model has 100% of the raw data at the precise moment it writes the final code or answer. Intermediate turns save 65%–75% of cumulative tokens without any reasoning degradation.

---

## 3. Deterministic Prefix Canonicalization

Modern LLM providers (Anthropic prompt cache, OpenAI prefix cache) offer 50% to 90% price discounts when prompt prefixes are byte-for-byte identical across calls.

However, client SDKs and agent frameworks frequently break caching:
* Tool schemas are serialized in non-deterministic dictionary order.
* Dynamic timestamps or volatile headers are injected into system prompts.
* Variable JSON whitespace causes cache misses.

In `internal/proxy/canonicalizer.go`, Loopers canonicalizes requests before hashing or upstream dispatch:
* **Tool Schema Sorting**: Re-orders tools and function parameters alphabetically by key.
* **Whitespace Normalization**: Collapses superfluous whitespace and normalizes JSON separators.
* **Stable System Anchors**: Strips volatile runtime metadata from prefix boundaries.

This elevates KV-cache hit rates to **85%–90%** on multi-turn conversations, compounding savings on top of Ghost Tokens.

---

## 4. Smart Fast-Tier Routing & Warm Cache Affinity

In `internal/server/router.go`, Loopers evaluates incoming turns against model pricing policies configured in `pricing.yaml`:

* **Intermediate Verification Turns**: When an agent performs simple verification turns (such as formatting a JSON output, answering a binary check `is_multi_select: false`, or confirming a status), Loopers can route the turn to a high-speed, cost-efficient model (e.g., `deepseek-v4.1-flash`, `gpt-4o-mini`, `claude-3-5-haiku`).
* **Warm Cache Pinning**: If the prompt prefix is already cached in the frontier model's KV cache (e.g., Claude 3.5 Sonnet), Loopers preserves model pinning to capture the 90% cache read discount, rather than triggering a cold hop to a fast-tier model.

---

## Configuration Reference

Enable and tune Inline Context Acceleration in `loopers.yaml`:

```yaml
# Inline Context Acceleration & Wire Optimization
ghost_tokens:
  enabled: true
  threshold_bytes: 512          # Offload payloads larger than 512 bytes
  ttl_seconds: 3600             # Ephemeral Redis retention (1 hour)
  encryption_secret: "env:LOOPERS_GHOST_SECRET" # AES-256-GCM secret key

sanitizer:
  enabled: true
  minify_schemas: true          # Strip verbose descriptions from stable tool schemas
  memoize_read_only: true       # Cache read-only tool responses

# Fast-Tier Routing in pricing.yaml
model_routing:
  enabled: true
  fast_tier_model: "gpt-4o-mini"
  frontier_model: "gpt-4o"
```

---

## Request & Response Headers

### Request Controls

| Header | Description |
|---|---|
| `X-Loopers-Skip-Hydration: true` | Forces Loopers to keep ghost stubs without decrypting raw data on the current turn. |
| `X-Loopers-Force-Frontier: true` | Bypasses fast-tier routing, forcing dispatch to the configured frontier model. |

### Telemetry Response Headers

| Header | Description |
|---|---|
| `X-Loopers-FinOps-Route` | Indicates the executed tier (e.g., `"fast-tier"` or `"frontier"`). |
| `X-Loopers-FinOps-Throttled` | Set to `"true"` when output tokens were bounded on simple intermediate turns. |
| `X-Loopers-Cache-Read-Tokens` | Number of tokens served from upstream provider KV cache. |
| `X-Loopers-Cache-Write-Tokens` | Number of tokens written to upstream provider KV cache. |
