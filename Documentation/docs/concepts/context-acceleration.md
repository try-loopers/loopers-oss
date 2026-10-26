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

Loopers intercepts large tool execution payloads on the MCP outbound response path in `internal/mcp/handler.go` and offloads them into an ephemeral side-channel:

```
┌────────────────────────────────────────────────────────────────────────┐
│ Agent Client                                                           │
│ [Turn 3]: Message history with 15KB file read from Turn 2              │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Inbound HTTP Request
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ Loopers AI Firewall (internal/proxy/ghost.go)                          │
│ 1. Evaluates incoming turns for synthesis vs simple execution          │
│ 2. Dynamic Tail-Hydration restores raw blobs when frontier synthesizes │
│ 3. Offload occurs at MCP response layer (internal/mcp/handler.go)      │
│ 4. Offload to Redis: loopers:blob:{keyHash}:{sessionID}:{blobID}       │
│ 5. Returns wire content with compact Ghost Stub                        │
└───────────────────┬──────────────────────────────────┬─────────────────┘
                    │                                  │
                    ▼ Raw Payload (AES-256-GCM)        ▼ Compressed Prompt (~80 tokens)
          ┌───────────────────┐              ┌───────────────────┐
          │      Redis        │              │ Upstream Provider │
          │   loopers:blob    │              │ (Anthropic/OpenAI)│
          └───────────────────┘              └───────────────────┘
```

### The Ghost Stub

The raw 15,000-token tool response is replaced on the wire by a compact, schema-compliant JSON stub (`GhostStub` in `internal/proxy/ghost.go`):

```json
{
  "_loopers_ref": "loopers:blob:7f83b1657ff1fc53",
  "tool": "read_file",
  "original_bytes": 15360,
  "est_tokens_saved": 3840,
  "structure": {
    "type": "file",
    "extension": "go"
  },
  "preview": "package main\n\nimport (\n..."
}
```

The LLM still understands that the tool execution succeeded and can reference the `_loopers_ref`, but the prompt token payload drops from 15,000 tokens to under 80 tokens.

---

## 2. Dynamic Tail-Hydration (Zero Quality Loss)

A common concern with token pruning is: *If the model cannot see the raw tool output on later turns, how can it generate an accurate final answer or report?*

Loopers solves this with **Dynamic Prompt Tail-Hydration** (`HydratePromptTail`):

1. **Turn Classification**: Loopers inspects the agent's message structure and intent. Intermediate turns (e.g. navigation, verification checks, or issuing subsequent tool calls) execute with compressed Ghost Stubs.
2. **Frontier Synthesis Detection**: When the agent reaches a synthesis or final completion turn (`!isSimpleTurn`), Loopers recognizes that full context is required.
3. **Just-in-Time Decryption**: Loopers fetches the referenced ghost blobs from Redis, decrypts them in volatile memory via ephemeral HKDF-SHA256 derived keys, and injects them into an explicit context container at the tail of the prompt:

```markdown
<loopers_hydrated_context>
[Turn 2 tool_call: read_file]
... full raw decrypted file contents (15,000 tokens) ...
</loopers_hydrated_context>
```

4. **Zero Quality Regressions**: The frontier model has 100% of the raw data at the precise moment it writes the final code or answer. Intermediate turns save 65% to 75% of cumulative tokens without any reasoning degradation.

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

This elevates KV-cache hit rates to **85% to 90%** on multi-turn conversations, compounding savings on top of Ghost Tokens.

---

## 4. Smart Fast-Tier Routing & Warm Cache Affinity

In `internal/server/router.go`, Loopers evaluates incoming turns against model pricing policies configured in `pricing.yaml`:

* **Intermediate Verification Turns**: When an agent performs simple verification turns (such as formatting a JSON output, answering a binary check `is_multi_select: false`, or confirming a status), Loopers can route the turn to a high-speed, cost-efficient model (e.g., `deepseek-v4.1-flash`, `gpt-4o-mini`, `claude-3-5-haiku`).
* **Warm Cache Pinning**: If the prompt prefix is already cached in the frontier model's KV cache (e.g., Claude 3.5 Sonnet), Loopers preserves model pinning to capture the 90% cache read discount, rather than triggering a cold hop to a fast-tier model.

---

## Configuration Reference

Enable and tune Inline Context Acceleration in `loopers.yaml`:

```yaml
# Inline Context Acceleration & MCP Wire Optimization
mcp:
  enabled: true
  ghost_tokens:
    enabled: true
    threshold_bytes: 8192        # Offload tool outputs larger than 8 KB (configurable 512B to 64KB)
    ttl_seconds: 3600           # Ephemeral Redis retention (1 hour)
  sanitizer:
    minify_schemas: true        # Strip verbose descriptions from stable tool schemas
  memoize_read_only: true       # Cache idempotent read-only tool responses

# Fast-Tier Routing is configured per model in pricing.yaml:
pricing_path: "./pricing.yaml"
```

In `pricing.yaml`, specify `fast_tier` targets for frontier models:

```yaml
providers:
  anthropic:
    models:
      claude-3-5-sonnet-20241022:
        tier: "frontier"
        fast_tier: "claude-3-5-haiku-20241022"
        input_per_1m: 3.00
        output_per_1m: 15.00
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
| `X-Loopers-Saved-USD` | Cumulative USD saved on the request via arbitrage, ghost tokens, or memoized tools. |
| `X-Loopers-Ghost-Token` | Blob ID emitted when an outbound tool result is offloaded to a ghost stub. |
| `X-Loopers-MCP-Memoized` | Set to `"true"` when an idempotent tool call was served directly from cache. |
