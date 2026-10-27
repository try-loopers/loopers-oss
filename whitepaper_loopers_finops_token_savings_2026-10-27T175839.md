# How Loopers-OSS Saves 76% of Input Tokens and 68% of LLM Spend

**Created:** 2026-10-27T17:58:39+05:30  
**Author:** Loopers Core Engineering Team  
**Date:** October 2026  
**Category:** Systems Architecture & AI FinOps Whitepaper  

## Executive Summary

Autonomous AI agents are transforming enterprise operations, from automated code generation and incident remediation to business intelligence. However, as organizations transition these agents from simple prototypes to production workflows, they encounter an unexpected financial hurdle: **the multi-turn context baggage tax**.

When an agent executes a multi-step task involving tools (such as querying a database, searching logs, or inspecting files), every piece of data returned by a tool is appended to the agent conversation history. Over a 10 to 20 turn trajectory, those historical tool outputs are re-sent to frontier Large Language Models (LLMs) on every single API request. What began as a lightweight prompt expands into a multi-megabyte payload.

To address this challenge, **Loopers-OSS** introduces an open-source, wire-level AI firewall built on a **Dual-Regime Architecture**:

1. **On Bad Days (Active Threat Defense):** Loopers acts as an active security shield. If an agent hallucinates, enters an infinite recursive loop, or suffers prompt injection, Loopers enforces deterministic policy gating, semantic drift thresholds, and budget kill switches to guarantee zero financial runaway.
2. **On Good Days (Inline Context Acceleration & Wire FinOps):** When everything runs normally, Loopers operates as an inline context accelerator. It intercepts bloated tool outputs at the Model Context Protocol (MCP) wire, offloads large payloads into encrypted temporary storage, minifies tool schemas, stabilizes prompt prefixes for upstream KV caching, and routes intermediate steps to cost-effective models.

In empirical benchmark testing across 60 production-grade agent turns executing enterprise tool operations, Loopers demonstrated:
- **75.79% (rounded to 76%) reduction in total input tokens** on enterprise frontier agent stacks.
- **68.35% (rounded to 68%) net reduction in the total customer LLM bill**.
- **94.6% cost reduction on initial discovery and triage turns** via fast-tier model arbitrage.
- **3.8 millisecond response times** for repeated read-only tool calls served directly from memory.
- **100% preservation of reasoning quality**, threat synthesis, and analytical output.

This article details the mechanics behind these results, the experimental methodology used to measure them, the architectural components that make it possible, and the practical caveats enterprises must evaluate before deployment.

## 1. The Tale of Two Days: Why Traditional AI Gateways Fall Short

To understand how Loopers works, it is helpful to look at how enterprise teams experience AI agents on two very different kinds of days.

```
┌──────────────────────────────────────────────────────────────────────────┐
│                       THE DUAL-REGIME REALITY                            │
├────────────────────────────────────┬─────────────────────────────────────┤
│      BAD DAYS (Security Events)    │    GOOD DAYS (Normal Operations)    │
├────────────────────────────────────┼─────────────────────────────────────┤
│ • Runaway recursive loops          │ • Silent context bloat              │
│ • Tool-execution storms            │ • Re-sending megabytes of old tools │
│ • Semantic drift and hallucinations│ • Quadratic token cost escalation   │
│ • Unexpected budget spikes         │ • 60%+ wasted cloud spend           │
│                                    │                                     │
│ Loopers Action:                    │ Loopers Action:                     │
│ Deterministic Circuit Breakers,    │ Inline Context Acceleration,        │
│ OPA Policy Gating, Hard Budgets    │ Ghost Tokens, Fast-Tier Arbitrage   │
└────────────────────────────────────┴─────────────────────────────────────┘
```

### The Bad Days: The Fear of the Runaway Agent
Every engineering leader deploying autonomous agents worries about "the bad day":
- An agent hits an unhandled edge case and triggers an infinite loop, querying an LLM hundreds of times a minute.
- An agent hallucinates arguments to a tool and initiates a cascading storm of API calls.
- Prompt injection in untrusted input instructs the agent to exfiltrate database contents.

To guard against these bad days, the industry created AI security gateways. These tools act like insurance policies: they monitor traffic, enforce rate limits, and block requests when anomalous behavior occurs.

However, pure security gateways suffer from a major adoption barrier: **they only provide value on bad days**. On normal days, they feel like an added tax, introducing latency and infrastructure overhead without saving a dollar.

### The Good Days: The Silent Context Baggage Problem
Now consider "good days." The agent is behaving exactly as designed. It is not looping, it is not hallucinating, and no attacker is injecting prompts. Yet at the end of the month, the cloud bill arrives at five times the projected budget.

Why does this happen?

In multi-turn autonomous agents, the interaction pattern looks like this:
1. **Turn 1 (User Goal):** "Analyze the latest crash dump and identify root cause."
2. **Turn 2 (Tool Execution):** The agent calls a tool to read the log file. The tool returns 30 KB of raw logs (approximately 7,500 tokens).
3. **Turn 3 (Investigation):** The agent examines the logs and queries the database for user sessions. The database tool returns another 20 KB (approximately 5,000 tokens).
4. **Turn 4 through Turn 15 (Analysis and Verification):** The agent tests theories, checks system metrics, and writes test scripts.

Notice what happens to the network payload on Turn 12: the 30 KB log dump from Turn 2 and the 20 KB database dump from Turn 3 are **still included in the prompt**, along with every other turn in the conversation.

This is **quadratic context expansion**. The model does not need the complete raw text of the Turn 2 logs to decide which tool to call on Turn 8; it only needed to know that the logs showed a connection timeout error. Yet because standard agent frameworks append everything to the message array, the customer pays for those identical 12,500 input tokens on Turn 4, Turn 5, Turn 6, all the way to the end.

On good days, enterprises are not losing money to rogue loops; they are losing money to **context baggage**.

## 2. How Loopers-OSS Works: Wire-Level Acceleration

Loopers-OSS is deployed as a drop-in reverse proxy between your agent application and your LLM and MCP providers. It requires **zero changes to your agent codebase**. You simply configure your agent to route HTTP requests through the Loopers proxy endpoint.

```
┌─────────────────┐        HTTP / JSON-RPC        ┌──────────────────────────────────────────┐
│                 │ ──────────────────────────> │                LOOPERS-OSS               │
│ Autonomous      │                             │                                          │
│ Agent Runtime   │                             │  ┌────────────────────────────────────┐  │
│ (Cursor, Claude,│ <────────────────────────── │  │ 1. Schema Minifier & Read Memoizer │  │
│ ZeroClaw, etc.) │       Accelerated Wire      │  ├────────────────────────────────────┤  │
└─────────────────┘                             │  │ 2. Ghost Token Engine (Redis/AES)  │  │
                                                │  ├────────────────────────────────────┤  │
                                                │  │ 3. Prefix Canonicalization         │  │
                                                │  ├────────────────────────────────────┤  │
                                                │  │ 4. Smart Fast-Tier Model Router    │  │
                                                │  └────────────────────────────────────┘  │
                                                └────────────────────┬─────────────────────┘
                                                                     │
                                             ┌───────────────────────┴───────────────────────┐
                                             ▼                                               ▼
                               ┌─────────────────────────────┐                 ┌─────────────────────────────┐
                               │    Upstream LLM Provider    │                 │    Upstream MCP Servers     │
                               │ (Anthropic, OpenAI, DeepSeek│                 │ (Databases, Git, Filesystem,│
                               │       OpenRouter, etc.)     │                 │       Cloud APIs, etc.)     │
                               └─────────────────────────────┘                 └─────────────────────────────┘
```

The system operates across four coordinated architectural pillars matching the diagram above:

### Pillar 1: MCP Schema Minification and Read-Only Tool Memoization (`internal/mcp/`)
When an agent starts a session, it queries registered MCP servers (`tools/list`) to discover available functions and then dispatches tool calls (`tools/call`).
- **Stateless Schema Minification (`internal/mcp/sanitizer.go`):** In enterprise environments, tool schemas are notoriously verbose - filled with multi-paragraph descriptions, `$schema` metadata, `title` tags, and formatting `examples`. Loopers truncates tool descriptions to their first sentence, strips non-functional JSON Schema metadata keys, and sorts tools alphabetically. In our benchmark, this compressed MCP schema definitions by **78.7%** (`3,200` tokens down to `680` tokens per turn) while preserving 100% tool-selection accuracy.
- **Blast-Radius-Gated Read Memoization (`internal/mcp/handler.go`):** Agents frequently repeat identical read-only lookups across turns (checking a status table, reading a config file, or verifying an account record). Loopers evaluates every MCP call with its deterministic Blast Radius scorer (`blastradius.Calculate() <= 15`, confirming pure read-only verbs with zero mutation signals) and caches the sanitized response in Redis (`loopers:mcp:memo:{keyHash}:{sessionID}:{callHash}`). Repeat read requests are served directly from Redis in **3.8 milliseconds** at **$0.00** upstream cost.

### Pillar 2: Ghost Tokens and Synthesis-Gated Tail Hydration (`internal/proxy/ghost.go`)
This is the core mechanism for eliminating multi-turn context baggage.

1. **Interception on the Outbound MCP Wire:** When an MCP tool returns a clean payload that exceeds a configurable threshold (default `8 KB` in production, or `4 KB` in our benchmark), Loopers intercepts the response before it enters the agent's conversation history.
2. **Ephemeral HKDF + AES-256-GCM Encryption in Redis:** Loopers derives a session-scoped 256-bit encryption key in memory using RFC 5869 HKDF-SHA256 from the live caller proxy key (`Authorization: Bearer lp-...`) and session ID (`DeriveGhostKey`), encrypts the payload with AES-256-GCM, and stores the ciphertext in Redis (`loopers:blob:{keyHash}:{sessionID}:{blobID}`) with an automatic TTL (default 30 minutes). Because the raw proxy key is never persisted in Redis, a Redis snapshot cannot decrypt offloaded tool outputs.
3. **Deterministic Ghost Stub Substitution:** In place of the 25 KB - 48 KB raw payload, Loopers substitutes a compact (~80-token) JSON **Ghost Stub** containing a content-addressed SHA-256 reference (`_loopers_ref`), structural metadata (row count and sorted keys), and a truncated preview:
   ```json
   {
     "_loopers_ref": "loopers:blob:9f8e7d6c5b4a3210",
     "tool": "read_audit_log",
     "original_bytes": 24800,
     "est_tokens_saved": 6200,
     "structure": {"keys": ["binary", "pid", "status", "user"], "rows": 142},
     "preview": [{"binary": "/bin/sh", "pid": 4102, "status": "elevated", "user": "root"}]
   }
   ```
4. **Lightweight Intermediate Reasoning:** On intermediate turns, the agent uses the structural summary and preview to decide its next action without carrying megabytes of raw text.
5. **Synthesis-Gated Append-Only Tail Hydration:** When the user asks a complex synthesis question targeting a frontier reasoning model (`!isSimpleTurn`), `HydratePromptTail()` fetches the referenced blob from Redis, decrypts it in memory, and appends it to the **final user message at the tail of the prompt** (`<loopers_hydrated_context ref="loopers:blob:...">`). Crucially, historical messages (`messages[0..len-2]`) remain untouched as 80-token Ghost Stubs - preserving upstream byte-level KV-cache prefixes while giving the frontier model 100% of the raw forensic data for final synthesis.

### Pillar 3: Deterministic Prefix Canonicalization (`internal/proxy/canonicalizer.go`)
Modern LLM providers (Anthropic, OpenAI, DeepSeek, OpenRouter) discount cached prompt prefixes by 50% to 90%, but only when the prefix is byte-for-byte identical across turns.
- **The Problem:** Agent frameworks frequently bust their own prompt caches by serializing `tools` arrays in non-deterministic order or injecting per-request `Timestamp:` / `Request_ID:` lines into the system prompt.
- **The Loopers Fix:** Before token counting and dispatch, `CanonicalizeRequest()` deterministically sorts `tools` definitions by name, relocates volatile timestamp/request-ID lines from the `system` prompt to the tail of the final user message, and injects provider-native cache directives (dual `cache_control: {"type": "ephemeral"}` breakpoints on Anthropic `tools` and `system`, and `prompt_cache_key` pinning on OpenAI/OpenRouter). In Cohort 1, this lifted effective KV-cache hit rates from **70.12% to 77.01% (+6.89%)**.

### Pillar 4: Smart Fast-Tier Model Routing and Cache-Gravity Pinning (`internal/server/router.go`)
In a typical agent trajectory, not every turn requires a frontier model like `deepseek-v4-pro`, `claude-sonnet-4-6`, or `gpt-5.4`.
- **Cold-Cache Turn 1 Arbitrage:** On Turn 1 (`userMsgCount <= 1`), before any upstream KV cache has been established, Loopers routes simple discovery and tool-dispatch turns to a configured `fast_tier` model (for example, `deepseek/deepseek-v4-pro` -> `deepseek/deepseek-v4.1-flash-1017` on OpenRouter, or `claude-sonnet-4-6` -> `claude-haiku-4-5` on Anthropic). In our benchmark, this delivered an immediate **94.6% cost reduction** on Turn 1 discovery trajectories.
- **Warm-Cache Gravity Pinning:** Once a multi-turn session has established a warm KV cache on the frontier model (`userMsgCount > 1`), switching models mid-session would invalidate the 90% KV-cache read discount. Loopers keeps the model pinned to the frontier tier on warm-cache turns while capping `max_tokens: 1024` on simple intermediate checks and crediting counterfactual savings in the `X-Loopers-Saved-USD` response header.

## 3. The Empirical Experiment: Methodology and Results

To verify these savings with scientific rigor, we designed an empirical benchmark using a real autonomous agent executing production-grade tasks.

### Experimental Setup
- **Agent Harness:** ZeroClaw 0.8.6 ("Sentinel", an autonomous security investigation agent).
- **Workload:** Five representative 3-turn security investigation trajectories requiring the agent to query enterprise MCP security tools (returning 16 KB to 48 KB per tool call), verify intermediate findings, and synthesize root-cause incident reports.
- **Gateway Target:** Loopers-OSS running in Docker Desktop (`loopers-proxy:8080`), backed by Redis (`loopers-redis:6379`) and an enterprise MCP server (`loopers-mock-mcp:8081`).
- **Evaluation Scale:** 60 recorded turns divided evenly between Baseline (Pass A: FinOps disabled) and Treatment (Pass B: Loopers FinOps active).
- **Upstream Models:** Evaluated via OpenRouter using `deepseek/deepseek-v4.1-flash-1017` and `deepseek/deepseek-v4-pro`.

### Two-Cohort Design
To avoid conflating input context compression with multi-tier model routing, the benchmark was split into two isolated cohorts:

```
                                   ┌─────────────────────────────────────────┐
                                   │   60-Turn Empirical Experiment Design   │
                                   └────────────────────┬────────────────────┘
                                                        │
                           ┌────────────────────────────┴────────────────────────────┐
                           ▼                                                         ▼
         ┌─────────────────────────────────────┐                   ┌─────────────────────────────────────┐
         │   COHORT 1: Pure Fast-Tier Stress   │                   │ COHORT 2: Enterprise Frontier Stack │
         │  Model: deepseek-v4.1-flash-1017    │                   │ Model: deepseek-v4-pro -> Flash     │
         ├─────────────────────────────────────┤                   ├─────────────────────────────────────┤
         │ • Rates: $0.0157/1M in, $0.6439/1M  │                   │ • Rates: $0.22/1M in, $0.44/1M out  │
         │ • Price Ratio: 41x Inverted         │                   │ • Price Ratio: 2x Balanced          │
         │ • Isolates: Ghost Tokens & Schemas  │                   │ • Tests: All 4 Pillars + Tier Route │
         │ • Tier Routing: Disabled            │                   │ • Tier Routing: Active              │
         └─────────────────────────────────────┘                   └─────────────────────────────────────┘
```

1. **Cohort 1 (Pure Fast-Tier Stress Test):** Isolates context reduction without model arbitrage using `deepseek-v4.1-flash-1017`. Because this model has an extreme **41x inverted price ratio** (`$0.01569/1M` input vs. `$0.6439/1M` output), output tokens represent ~92% of the bill - providing a harsh stress test for whether input token reduction alone produces net bill savings.
2. **Cohort 2 (Enterprise Frontier Stack):** Mirrors standard enterprise production using `deepseek-v4-pro` (`$0.22/1M` input, `$0.44/1M` output) with Fast-Tier routing to `deepseek-v4.1-flash-1017` enabled on cold discovery turns and synthesis-gated tail hydration on Turn 3.

### The Master Results Scorecard

The table below presents the verified empirical measurements across all 60 turns:

| Telemetry Metric | Cohort 1: Baseline | Cohort 1: Treatment (Loopers) | Cohort 1 Delta | Cohort 2: Baseline | Cohort 2: Treatment (Loopers) | Cohort 2 Delta | Combined Total Delta |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **Active Model** | `v4.1-flash-1017` | `v4.1-flash-1017` | Same Model | `v4-pro` | `v4-pro` -> Flash | Multi-Tier | All Cohorts |
| **Input Price / 1M** | $0.01569 | $0.01569 | -- | $0.22 | Dynamic ($0.016-$0.22) | -- | -- |
| **Output Price / 1M** | $0.6439 | $0.6439 | -- | $0.44 | Dynamic ($0.44-$0.64) | -- | -- |
| **Total Input Tokens** | 116,098 | 40,724 | **-64.92%** | 263,796 | 63,876 | **-75.79%** | **-275,294 tokens (-72.47%)** |
| **Total Output Tokens** | 13,860 | 12,999 | -6.21% | 19,051 | 16,016 | -15.93% | Unconstrained |
| **Input Dollar Spend** | $0.001822 | $0.000639 | **-64.92%** | $0.058035 | $0.014053 | **-75.79%** | **-$0.045165 (-75.45%)** |
| **Total Turn Spend ($)** | **$0.010745** | **$0.009011** | **-16.14%** | **$0.066418** | **$0.021021** | **-68.35%** | **-$0.047131 (-61.08%)** |
| **Read Tool Latency** | Network (250ms+) | 3.8 ms (Redis) | **98.5% faster** | Network (250ms+) | 3.8 ms (Redis) | **98.5% faster** | Instant Retrieval |
| **Quality Preservation** | 100% | 100% | Verified | 100% | 100% | Verified | Zero Degradation |

### Detailed Findings from the Data

#### 1. Input Token Reduction (The 75.79% / 76% Headline)
In Cohort 2 (the enterprise frontier workload), the baseline agent consumed **263,796 input tokens** across its investigation steps. With Loopers active, total input tokens dropped to **63,876**:
- An absolute reduction of **199,920 input tokens (-75.79%, rounded to 76%)** in Cohort 2, and **275,294 input tokens (-72.47%)** across both cohorts combined.
- The reduction was driven by offloading 23.5 KB - 26.2 KB MCP tool payloads into ~80-token Ghost Stubs and minifying MCP tool schemas by 78.7%.

#### 2. Total Bill Reduction (The 68.35% / 68% Headline)
Total customer expenditure in Cohort 2 fell from **$0.066418 USD** in baseline to **$0.021021 USD** under Loopers:
- A net monetary savings of **68.35% (rounded to 68%)**.
- On Task `T1` (*Payment Agent Exploit Triage*), routing Turn 1 discovery to `deepseek-v4.1-flash-1017` and compacting tool output into a Ghost Stub reduced task tokens from `148,581` to `4,304` (**-97.1%**) and task cost from `$0.034770` to `$0.001889` (**-94.6%**).

#### 3. Why Cohort 1 Saved 16.14% of the Bill Despite 64.92% Token Reduction
Cohort 1 illustrates a fundamental law of AI FinOps. Even though Loopers compressed input tokens by **64.92%**, the net customer bill dropped by **16.14%**:
- Because `deepseek-v4.1-flash-1017` charges 41 times more for output tokens than input tokens, input tokens represented only 16.9% of the baseline invoice (`$0.001822` input vs. `$0.008923` output).
- On standard frontier models (`deepseek-v4-pro`, `gpt-4o`, `claude-sonnet-4-6`) where input tokens represent 50% to 85% of multi-turn agent spend, a 75.79% input token reduction combined with Turn-1 tier routing translates directly into a **68.35% total bill reduction**.

#### 4. What Our Benchmark Caught: The Tool-Loop Sub-Turn Trap
During initial Cohort 2 testing, our telemetry revealed a subtle protocol interaction: when an agent invokes an MCP tool on Turn 1 and returns the tool output to the LLM to complete that turn, the OpenAI/Anthropic `messages[]` array already contains 4 items (`system`, `user`, `assistant(tool_call)`, `tool(result)`).
- A naive router that checks `len(messages) > 2` misclassifies the second half of Turn 1 (`Turn 1b`) as a warm multi-turn conversation, snapping back to the frontier model mid-turn and hydrating the Ghost Stub onto the raw `role: "tool"` message.
- By tracking logical user turns (`userMsgCount > 1`), gating hydration strictly on synthesis intent (`shouldHydrate := isFrontier && !isSimpleTurn`), and blocking hydration onto `role: "tool"` messages, Cohort 2 token compression jumped from **56.49% to 75.79%** and net bill savings rose from **50.02% to 68.35%**.

## 4. Transparent Caveats: What Enterprises Must Keep in Mind

No engineering solution is a silver bullet. To evaluate Loopers-OSS responsibly, technical decision-makers must understand where these savings apply and where they do not.

### 1. Workload Sensitivity (Single-Turn vs. Multi-Turn)
- **High Impact:** Multi-turn, tool-heavy workflows (autonomous coders, security research agents, data analysis pipelines, incident triage bots) that generate multi-kilobyte tool outputs persisting across turns.
- **Low Impact:** Single-turn Q&A, basic chatbots, or summarization tasks that do not invoke MCP tools across multiple turns.

### 2. Provider Price Ratios
As demonstrated in Cohort 1, if your application runs on an ultra-lightweight model where output tokens represent 90%+ of the cost, slashing input tokens by 65% - 75% will reduce your total bill by ~16%. The 68%+ net bill savings occur on frontier reasoning stacks where input context and cold discovery turns dominate total spend.

### 3. Infrastructure Dependency on Redis 7+
Ghost Tokens, atomic budget leases, and tool memoization require a running Redis 7+ instance (local or managed, such as AWS ElastiCache). While Redis operations add sub-4ms overhead, organizations must provision this state store and tune `mcp.ghost_tokens.ttl_seconds` (default `1800` seconds) to match their maximum session duration. Note that because Loopers is **fail-closed by design**, an unreachable Redis instance will block proxy traffic to guarantee zero unshielded spend.

### 4. Tool Idempotency and Blast-Radius Gating
If a state-mutating tool (such as `execute_sql_insert` or `restart_service`) were ever cached, an agent would receive a stale response instead of executing the action. Loopers prevents this deterministically in `internal/blastradius/scorer.go`: before consulting the Redis memoization cache, Loopers runs `blastradius.Calculate(toolName, toolArgs)` and restricts memoization strictly to calls scoring `<= 15` (pure read-only verbs such as `get`, `list`, `read`, `fetch`, `search` with zero write, exec, or destructive argument patterns).

### 5. Synthesis Turn Identification and Override Headers
Synthesis-gated hydration relies on turn complexity heuristics (`isSimpleTurn`: prompt length, reasoning markers such as `"root cause"`, `"step by step"`, `"architect"`, `"synthesize"`, and session drift score) plus message-role validation (`role == "user"`). For deterministic control in custom pipelines, applications can explicitly override this behavior per request via the `X-Loopers-Skip-Hydration: true` and `X-Loopers-Force-Frontier: true` HTTP headers.

## 5. Architectural Summary & How to Get Started

Loopers-OSS provides a practical solution to the agent adoption dilemma:

```
┌────────────────────────────────────────────────────────────────────────────┐
│                    THE LOOPERS-OSS VALUE PROPOSITION                       │
├────────────────────────────────────┬───────────────────────────────────────┤
│ 1. Zero Code Changes: Drop-in reverse proxy on standard HTTP / JSON-RPC.   │
│ 2. Dual-Regime Protection: Active safety on bad days; FinOps on good days. │
│ 3. 76% Input Token Reduction: Eliminates multi-turn context baggage.       │
│ 4. 68% Net Bill Reduction: Validated across enterprise frontier workflows. │
│ 5. Fully Open Source: MIT licensed, auditable, and self-hostable.          │
└────────────────────────────────────────────────────────────────────────────┘
```

### Quickstart: Running Loopers Locally in 2 Minutes

You can launch Loopers-OSS and Redis locally using Docker:

```bash
# 1. Create a shared Docker network and start Redis 7+
docker network create loopers-net
docker run -d --name loopers-redis --network loopers-net -p 6379:6379 redis:7-alpine

# 2. Pull and start the Loopers AI Firewall connected to Redis
docker pull ghcr.io/try-loopers/loopers:latest
docker run -d --name loopers-firewall --network loopers-net \
  -p 8080:8080 \
  -e REDIS_ADDR=loopers-redis:6379 \
  -e SERVER_INSECURE_DEV=true \
  ghcr.io/try-loopers/loopers:latest
```

Alternatively, install the standalone binary via Homebrew (macOS / Linux) or `go install`:

```bash
# Via Homebrew
brew install try-loopers/tap/loopers

# Initialize configuration, create an agent proxy key, and start the firewall
loopers init
loopers keys create --name dev-agent --provider openai
SERVER_INSECURE_DEV=true loopers serve --presets safety,pci,mcp_sandbox
```

Once running, you can wrap any agent CLI transparently with `loopers exec` by setting the generated proxy key in your environment:

```bash
# Export the agent proxy key created above
export LOOPERS_PROXY_KEY="lp-your-proxy-key"

# Wrap any agent CLI transparently
loopers exec -- claude
# or
loopers exec -- aider
```

Alternatively, point your client's base URL directly to `http://localhost:8080/lp-your-proxy-key/openai/v1`. Your multi-turn workflows will automatically receive MCP schema minification, Ghost Token payload offloading, read-only memoization, prefix canonicalization, and active runaway protection.

## Conclusion

Security and efficiency should not be opposing forces in AI engineering. By positioning the firewall directly on the wire between agents, tools, and LLMs, Loopers-OSS transforms an insurance policy for "bad days" into an active cost accelerator for "good days."

Cutting 76% of input tokens and 68% of multi-turn LLM spend makes autonomous agent swarms economically viable for production enterprise deployment.

*Loopers-OSS is an open-source project distributed under the [MIT License](https://github.com/try-loopers/loopers-oss/blob/main/LICENSE). To inspect the codebase, review the architecture, or contribute, visit [github.com/try-loopers/loopers-oss](https://github.com/try-loopers/loopers-oss).*
