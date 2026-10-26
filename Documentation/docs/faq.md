---
id: faq
title: Frequently Asked Questions (FAQ)
description: Common questions about Loopers AI Firewall, loop breaking, token budget enforcement, MCP governance, and zero-storage security.
---

import Head from '@docusaurus/Head';

<Head>
  <script type="application/ld+json">
    {JSON.stringify({
      "@context": "https://schema.org",
      "@type": "FAQPage",
      "mainEntity": [
        {
          "@type": "Question",
          "name": "What is Loopers?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "Loopers is an open-source, baremetal, zero-delay firewall for AI agents and Large Language Models (LLMs). It acts as a pass-through proxy to prevent token overspending, stop runaway agent loops, and govern Model Context Protocol (MCP) tool calls across 500+ AI models."
          }
        },
        {
          "@type": "Question",
          "name": "How does Loopers prevent runaway agent loops?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "Loopers uses a deterministic loop detection engine featuring Fingerprint Ring hashing, Velocity Limiters, and Stall Detectors to track repeated tool calls, message hashes, and execution frequencies in real time, severing connections instantly when a loop is detected."
          }
        },
        {
          "@type": "Question",
          "name": "How does Loopers compare to LiteLLM and Bifrost?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "Loopers is built in native Go for infrastructure-level speed, delivering 4,623 req/s throughput with sub-millisecond overhead and 0% budget leakage under concurrency, whereas LiteLLM is a Python proxy with higher latency overhead. Loopers uniquely provides agent loop circuit breakers, MCP tool-call governance, and inline context acceleration."
          }
        },
        {
          "@type": "Question",
          "name": "What are the two operational regimes of the Loopers AI Firewall?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "Loopers operates across two regimes: Regime 1 provides Rogue Agent Defense ('Bad Days' Protection) by circuit breaking runaway loops, objective drift, and budget spikes. Regime 2 provides Inline Context Acceleration ('Normal Days' Optimization) by offloading bulky tool outputs via Ghost Tokens, canonicalizing prompt prefixes for 90% cache hits, and fast-tier routing to cut everyday spend by 65% to 75%."
          }
        },
        {
          "@type": "Question",
          "name": "What are Ghost Tokens and do they degrade model reasoning?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "Ghost Tokens replace oversized tool payloads (>512 bytes) with encrypted cryptographic stubs in the prompt wire while storing the raw payload in ephemeral Redis. On intermediate navigation turns, this slashes input tokens by 65% to 75%. When an agent triggers a complex reasoning or final synthesis turn, Loopers automatically tail-hydrates the raw data, ensuring zero loss in model reasoning or output quality."
          }
        },
        {
          "@type": "Question",
          "name": "What is MCP Governance in Loopers?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "MCP Governance in Loopers intercepts JSON-RPC 2.0 traffic to Model Context Protocol servers, enforcing per-tool cost controls (e.g. max cost per tool execution) and stopping lateral agent movement or runaway tool iterations."
          }
        },
        {
          "@type": "Question",
          "name": "Is Loopers safe for sensitive API keys and data?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "Yes. Loopers implements a Zero-Storage Security Model. Your API keys and prompt payloads reside strictly in volatile memory during request proxying and are never persisted to disk or external databases. Ghost token payloads in Redis are encrypted using AES-256-GCM."
          }
        }
      ]
    })}
  </script>
</Head>

# Frequently Asked Questions (FAQ)

### What is Loopers?

**Loopers** is an open-source, baremetal, zero-delay firewall for AI agents and LLMs. It operates as a high-performance reverse proxy that sits between your applications/agents and model providers (OpenAI, Anthropic, Gemini, Groq, Bedrock, etc.) or MCP servers. It enforces real-time token budgets, detects and breaks infinite agent loops, and prevents bill shocks like LLMjacking.

---

### What are the two operational regimes of Loopers AI Firewall?

Loopers operates across two distinct regimes to maximize infrastructure safety and minimize costs:

1. **Regime 1: Rogue Agent Defense ("Bad Days" Protection)**: When agents malfunction, enter infinite recursive loops, suffer multi-turn objective drift, or leak credentials, Loopers trips active circuit breakers, clamps budget leases atomically, sanitizes prompt injections, and quarantines malicious agent keys.
2. **Regime 2: Inline Context Acceleration & Wire Optimization ("Normal Days" Optimization)**: When agents operate completely healthily, accumulating multi-turn tool outputs inevitably inflates prompt tokens and costs. Loopers transparently optimizes the wire:
   * **Ghost Tokens**: Offloads bulky tool responses (>512 bytes) to ephemeral Redis with AES-256-GCM encryption, replacing them with ~80-token ghost stubs to slash input tokens by **65% to 75%**.
   * **Deterministic Prefix Canonicalization**: Alphabetizes tool schemas and strips volatile whitespace to guarantee upstream KV-cache hits (up to 90% cache read discounts).
   * **Fast-Tier Routing**: Directs intermediate verification turns to lightweight models while preserving cache pinning on frontier reasoning turns.

---

### What are Ghost Tokens and does context compression affect response quality?

**No, response quality is not degraded.** 

In multi-turn agent runs, early tool execution outputs (such as large filesystem dumps or database query results) are typically only needed for intermediate steps (e.g. checking status, filtering items). Passing tens of thousands of tokens of raw JSON on every subsequent turn burns unnecessary compute and degrades LLM attention.

Loopers solves this with **Dynamic Prompt Tail-Hydration**:
* On intermediate turns, the raw payload is replaced with an encrypted cryptographic stub (`_loopers_ref: "loopers:blob:<id>"`).
* When the agent reaches a synthesis or reasoning turn requiring deep analysis (`!isSimpleTurn`), Loopers automatically decrypts the original context from Redis and appends it to the prompt tail (`<loopers_hydrated_context>`).
* The frontier model receives 100% of the raw, uncompressed context exactly when it needs to formulate the final answer, achieving **65% to 75% overall spend reduction with zero accuracy degradation**.

---

### How does Loopers stop runaway agent loops?

Loopers features a **Loop Detection Engine (v1.1)** running three deterministic algorithms:
1. **Fingerprint Ring**: Computes hashes of incoming message signatures, tool parameters, and response structures across sliding windows.
2. **Velocity Limiter**: Detects rapid-fire burst iterations that indicate an unconstrained recursive loop.
3. **Stall Detector**: Identifies non-progressive states where an agent repeatedly issues identical or redundant tool calls without converging on a solution.

When a loop is detected, Loopers severs the connection immediately, returning HTTP status `429` with header `X-Loopers-Loop-Detected: true`.

---

### How does Loopers compare to LiteLLM and Bifrost?

Loopers is engineered specifically as a baremetal **infrastructure-level firewall** prioritize speed, zero budget leakage, and agent governance.

| Feature | **Loopers** | LiteLLM | Bifrost |
| :--- | :--- | :--- | :--- |
| **Language / Architecture** | **Go (Native Binary)** | Python (FastAPI) | Go |
| **Peak Throughput** | **4,623 req/s** | ~176 req/s | ~3,200 req/s |
| **Budget Leakage (1k flood)** | **0% ($0.00)** | 0.17% | 0% |
| **P99 Proxy Overhead** | **240.98 ms** | 46,812.60 ms | ~310 ms |
| **Agent Loop Circuit Breaker** | **Yes** | No | No |
| **MCP Tool Governance** | **Yes (Per-tool)** | No | Policy-based |
| **Storage Security** | **Zero-Storage Pass-Through** | Database Required | In-Memory |

---

### What is MCP Governance in Loopers?

Model Context Protocol (MCP) governance allows enterprise and developer teams to safely expose external tools (databases, shell tools, APIs, web search) to autonomous AI agents. Loopers proxies JSON-RPC 2.0 requests between agents and MCP servers, enabling:
- **Per-Tool Cost Controls**: Set maximum USD spend per tool call (e.g. max `$0.05` per SQL query).
- **Circuit Breakers**: Cap the maximum tool invocations allowed within a single session.
- **Blast Radius Prevention**: Stop compromised or confused agents from performing lateral movement across multiple sensitive tools.

---

### Is Loopers zero-storage and safe for production data?

Yes. Loopers follows a **Zero-Storage Security Model**:
- No payload text, completions, or API keys are written to disk or database.
- Request headers and metadata are processed in volatile memory only.
- Budget accounting is recorded atomically in Redis using single-roundtrip Lua scripts without storing prompt content.

---

### How do I install and deploy Loopers?

Loopers can be deployed in under 60 seconds via Docker or binary release:

```bash
# Run with Docker
docker run -d \
  -p 8080:8080 \
  -e REDIS_ADDRESS=localhost:6379 \
  tryloopers/loopers:latest
```

Then point your provider SDK base URL to standard Loopers port:
```python
import openai

client = openai.OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="your-openai-key"
)
```
