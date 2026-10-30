---
id: cli
title: CLI Reference
sidebar_label: CLI Reference
description: Complete command reference for the Loopers CLI.
---

# CLI Reference

The loopers command line application allows you to manage keys, budgets, and the proxy server.

## Global Commands

| Command | Description |
|---|---|
| loopers | Interactive terminal UI (TUI) dashboard with live auto-refreshing FinOps Scorecard |
| loopers audit | Run a zero-config FinOps & Blast-Radius X-Ray on any agent workspace or MCP config |
| loopers init | Interactive setup wizard that generates loopers.yaml and docker-compose.yml |
| loopers serve | Start the proxy server |
| loopers mcp wrap | Transparently wrap local subprocess MCP tools over stdio with Schema Minifier & Ghost Tokens |
| loopers mcp bridge | Bridge local standard I/O (stdin/stdout) JSON-RPC agents to the Loopers HTTP firewall |
| loopers exec | Wrap and execute agent CLI binaries with transparent proxy injection |
| loopers verify | Audit JSON execution traces against OPA policies and Policy Cards |
| loopers doctor | Diagnose Loopers configuration and connectivity |
| loopers version | Print version information |

---

## Key Management

### loopers keys create

Create a new proxy key for a specific AI provider.

```bash
loopers keys create --name mykey --provider openai --agent-name research-agent --owner alice --tags "env=prod,team=alpha"
```

**Flags:**

| Flag | Type | Required | Description |
|---|---|---|---|
| --name | string | Yes | Name for the key |
| --provider | string | Yes | AI provider name (`openai`, `anthropic`, `gemini`, `bedrock`, `azure`, `mistral`, `groq`, `cohere`, `deepseek`, `together`, `ollama`, `fireworks`, `xai`, `vllm`, `openrouter`) |
| --agent-name | string | No | Name of the agent associated with this key |
| --owner | string | No | Owner of the key (person or team) |
| --allowed-tools | string | No | Comma-separated list of allowed MCP tools |
| --allowed-providers | string | No | Comma-separated list of allowed providers |
| --tags | string | No | Comma-separated key=value tags for policy evaluation (e.g., `env=prod,team=alpha`) |

**Output:**
```
Created key: mykey
  Raw Key:   lp-a1b2c3d4e5f6...  (Copy now, not shown again)
  Key Hash:  sha256:8f3a7b...
```

The `agent-name`, `owner`, and `tags` metadata are used by the [Policy Engine](/docs/guides/policy-engine) for ABAC evaluation and are included in OpenTelemetry spans and security events.

---

### loopers keys list

```bash
loopers keys list
```

Lists all proxy keys and their status.

---

### loopers keys revoke

```bash
loopers keys revoke KEY_HASH
```

Deactivates a key instantly using its hash. Future requests using the raw key will be rejected.

---

## Budget Management

### loopers budget set

```bash
loopers budget set KEY_HASH [flags]
```

**Flags:**

| Flag | Type | Description |
|---|---|---|
| --minute | float | Spending limit per minute in USD |
| --hourly | float | Spending limit per hour in USD |
| --daily | float | Spending limit per day in USD |
| --weekly | float | Spending limit per week in USD |
| --monthly | float | Spending limit per month in USD |

All flags are optional. The first limit you reach will block your requests.

**Example:**
```bash
loopers budget set KEY_HASH --minute 0.10 --daily 5.00 --monthly 100.00
```

---

### loopers budget status

```bash
loopers budget status KEY_HASH
```

**Output:**
```
KEY: KEY_HASH
  minute   spent=$0.04   limit=$0.10   (40%)
  daily    spent=$1.82   limit=$5.00   (36%)
  monthly  spent=$22.50  limit=$100.00 (22%)
```

---

## serve
```bash
loopers serve [flags]
```

Starts the Loopers AI Firewall proxy server. You can configure the server using the `loopers.yaml` configuration file, CLI flags, or environment variables.

**Flags:**

| Flag | Shorthand | Type | Default | Description |
|---|---|---|---|---|
| `--presets` | `-p` | string slice | `[]` | Built-in security & compliance presets to enable (`safety`, `safety_drift`, `pci`, `mcp_sandbox`, `zero_trust`, `owasp_llm_top10`, `nist_ai_rmf`, `eu_ai_act`) |
| `--policy-file` | `-f` | string | `""` | Path to declarative YAML Policy Card (e.g. `policies.yaml`) |
| `--policy-dir` | `-d` | string | `""` | Path to directory containing custom OPA Rego (`.rego`) files |
| `--config` | `-c` | string | `""` | Path to `loopers.yaml` configuration file |

> **Note**: `loopers serve` requires a valid TLS configuration (`server.tls_cert_file` and `server.tls_key_file`) in production. For local testing without TLS:

macOS / Linux:
```bash
SERVER_INSECURE_DEV=true loopers serve --presets owasp_llm_top10,nist_ai_rmf
```

Windows (PowerShell):
```powershell
$env:SERVER_INSECURE_DEV="true"; loopers serve --presets owasp_llm_top10,nist_ai_rmf
```

Windows (Command Prompt):
```cmd
set SERVER_INSECURE_DEV=true && loopers serve --presets owasp_llm_top10,nist_ai_rmf
```

To enable debug logging:

```bash
LOG_LEVEL=debug loopers serve
# or
loopers serve -v
```

---

## exec

```bash
loopers exec -- <command> [flags]
```

Executes a CLI agent command with the Loopers proxy environment automatically injected. This is the recommended way to integrate Loopers with any terminal-based AI agent.

**Required environment variables:**

| Variable | Description |
|---|---|
| `LOOPERS_PROXY_KEY` | Your Loopers proxy key (`lp-xxx`) |
| `LOOPERS_PROVIDER` | Upstream provider (`openai`, `anthropic`, `openrouter`, etc.). Auto-detected from executable name if omitted. |

**Optional flags:**

| Flag | Description |
|---|---|
| `--model-override <model>` | Force all requests to use a specific model (e.g. `google/gemma-2-9b-it:free`) |
| `--model-map <mappings>` | Remap specific model names by alias (e.g. `gpt-4o=google/gemini-2.5-pro`) |

**Example (macOS / Linux):**
```bash
export LOOPERS_PROXY_KEY="lp-xxx"
export LOOPERS_PROVIDER="openrouter"
export OPENAI_API_KEY="sk-or-v1-YOUR_KEY"

loopers exec --model-override "google/gemma-2-9b-it:free" -- opencode
```

**Example (Windows PowerShell):**
```powershell
$env:LOOPERS_PROXY_KEY="lp-xxx"
$env:LOOPERS_PROVIDER="openrouter"
$env:OPENAI_API_KEY="sk-or-v1-YOUR_KEY"

loopers exec --model-override "google/gemma-2-9b-it:free" -- opencode
```

---

## verify

```bash
loopers verify --trace <path-to-trace.json> [flags]
```

Audits and replays recorded execution traces (`.json`) against declarative YAML Policy Cards and OPA Rego policies to validate compliance offline.

**Flags:**

| Flag | Shorthand | Type | Default | Description |
|---|---|---|---|---|
| `--trace` | `-t` | string | `""` | **Required.** Path to session trace JSON file to verify |
| `--policy-file` | `-f` | string | `""` | Path to declarative YAML Policy Card (e.g., `policies.yaml`) |
| `--policy-dir` | `-d` | string | `""` | Path to directory containing custom Rego (`.rego`) policy files |
| `--presets` | `-p` | string slice | `[]` | Comma-separated list of built-in security & compliance presets (`safety`, `safety_drift`, `pci`, `mcp_sandbox`, `zero_trust`, `owasp_llm_top10`, `nist_ai_rmf`, `eu_ai_act`) |
| `--default-action` | | string | `"allow"` | Default decision when no policy rule matches (`allow` or `deny`) |
| `--format` | | string | `"pretty"` | Output format: `"pretty"` (terminal table) or `"json"` |
| `--fail-on-violation` | | bool | `true` | Exit with non-zero exit code (`1`) on detected policy violations |

**Example:**
```bash
loopers verify --trace ./traces/session.json --presets owasp_llm_top10,nist_ai_rmf,eu_ai_act
```

---

## Interactive TUI (loopers)

```bash
loopers
```

Launches an interactive, full-screen Bubble Tea terminal interface with keyboard navigation.

### FinOps Scorecard View

Select **FinOps Scorecard** from the main menu to open the real-time cost acceleration dashboard:
- **`● LIVE` Auto-Refresh**: Polls Redis every 1 second without terminal flickering.
- **Timeframe Hotkeys**:
  - `1`: Daily (Today)
  - `2`: Hourly (Current Hour)
  - `3`: Weekly (Current Week)
  - `4`: Monthly (Current Month)
  - `5`: All-Time Cumulative
  - `Tab`: Cycle through timeframes sequentially
  - `r`: Force manual refresh
  - `q` / `Esc`: Return to main menu
- **Metrics Displayed**:
  - **Total Spend**: Actual dollar spend billed by upstream providers.
  - **Counterfactual Spend**: What the workload would have cost without Loopers optimizations.
  - **Net Savings**: Dollar amount and percentage saved on the active timeframe.
  - **Ghost Tokens**: Total input tokens offloaded into Redis stubs and raw payload bytes cached.
  - **Fast-Tier Routing**: Turns routed to lightweight models and dollars saved.
  - **Tool Memoization**: Idempotent read calls served from cache and dollars saved.
  - **Schema Minifier**: Prompt discovery tokens stripped from `tools/list` responses.

---

## mcp wrap

```bash
loopers mcp wrap --server <name> --key <key> [flags] -- <command...>
```

Spawns a local MCP server subprocess, intercepts its JSON-RPC standard I/O streams, and transparently applies:
1. **Schema Minification**: Strips redundant JSON Schema boilerplate, markdown docs, and enum arrays from `tools/list` responses before passing them to the agent.
2. **Ghost Tokens**: Intercepts `tools/call` response payloads. If the payload exceeds `--threshold` (default 4KB), it is encrypted into Redis and replaced with an ~80-token Ghost Stub.
3. **Telemetry Recording**: Atomically records token savings directly to the active agent key's Redis FinOps registry.

**Flags:**

| Flag | Shorthand | Type | Default | Description |
|---|---|---|---|---|
| `--server` | `-s` | string | `"local"` | Logical name of the MCP server for metrics partitioning |
| `--key` | `-k` | string | `""` | Loopers agent proxy key (defaults to `LOOPERS_PROXY_KEY` env) |
| `--threshold` | `-t` | int64 | `4096` | Payload size threshold in bytes to trigger Ghost Token offloading |

**Example (ZeroClaw / Claude Desktop / Cursor):**
```bash
loopers mcp wrap --server notion --key lp-your-proxy-key -- npx.cmd -y mcp-remote https://mcp.notion.com/mcp
```

---

## loopers audit

```bash
loopers audit [directory] [flags]
```

Run an instant, zero-dependency diagnostic X-Ray of an AI agent workspace or Model Context Protocol configuration with **zero Redis, zero Docker, and zero proxy keys required**.

### Diagnostic Capabilities

1. **Static Context & Prefix Bloat**: Scans workspace prompt files (`SOUL.md`, `MEMORY.md`, `AGENTS.md`) and flags cache-busting volatile timestamps.
2. **MCP Schema Minification & Blast Radius**: Discovers MCP tool configs (`openclaw.json`, `zeroclaw.toml`, `.mcp.json`) or queries live HTTP MCP endpoints (`tools/list`). Minifies schemas to calculate compressible token bloat and calculates deterministic blast-radius risk scores (0–100) across all registered tools.
3. **Session Baggage Replay**: Replays `.jsonl` session histories to identify multi-kilobyte tool responses (>4KB) repeatedly resent on subsequent turns, computing tokens reclaimable via Ghost Token stubs.
4. **FinOps Scorecard & Actionable Conversion**: Computes estimated 30-day LLM spend vs. Loopers savings, assigns a Runaway Protection Grade, and provides `--fix` to generate a production-ready `loopers.yaml`.

### Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--fix` | boolean | `false` | Automatically generate a tailored `loopers.yaml` with discovered FinOps & security controls enabled |
| `--share` | boolean | `false` | Print a copy-pasteable Markdown table for GitHub Issues, Reddit, or X |
| `--provider` | string | `""` | LLM provider for cost calculation (`anthropic`, `openai`, `openrouter`, `deepseek`) |
| `--model` | string | `""` | Benchmark model to price against (e.g. `claude-sonnet-4-6`, `deepseek/deepseek-v4-pro`) |
| `--mcp-url` | string | `""` | Optional live HTTP MCP server URL to probe via `tools/list` |
| `--trace` | string | `""` | Optional path to a `.jsonl` session trace file or directory to replay |
| `--offline` | boolean | `false` | Skip remote pricing catalog sync and run strictly offline |

---

## mcp bridge

```bash
loopers mcp bridge --server <name> --key <key> [flags]
```

Provides a standard input/output (`stdio`) to HTTP bridge for local agents that only support stdio MCP servers, mapping JSON-RPC calls over stdin/stdout to the remote Loopers AI Firewall proxy endpoint (`/mcp/<server>/`).

**Flags:**

| Flag | Shorthand | Type | Default | Description |
|---|---|---|---|---|
| `--server` | `-s` | string | `""` | **Required.** Name of the target MCP server configured in `loopers.yaml` |
| `--key` | `-k` | string | `""` | Loopers agent proxy key (defaults to `LOOPERS_PROXY_KEY` env) |
| `--url` | `-u` | string | `"http://127.0.0.1:8080"` | Base URL of the Loopers AI Firewall proxy server |

**Example:**
```bash
loopers mcp bridge --server enterprise --key lp-your-proxy-key --url http://127.0.0.1:8080
```

