---
id: mcp-setup
title: Setting up MCP Governance
sidebar_label: MCP Setup
description: A user-friendly guide to configuring Loopers as an MCP firewall.
---

# Setting up Model Context Protocol (MCP) Governance

Loopers isn't just a firewall for your LLMs—it's also a firewall for your **Model Context Protocol (MCP)** tools. When autonomous agents interact with external systems (like databases, GitHub, or file systems), they can get stuck in infinite loops or accidentally rack up massive API bills.

Loopers solves this by acting as a transparent JSON-RPC proxy for your MCP servers. It enforces **per-tool budgets** and uses a **circuit breaker** to stop runaway loops in their tracks.

Here is a simple, step-by-step guide to setting it up.

---

## 1. Configure the MCP Servers

First, tell Loopers where your upstream MCP servers are running. You do this in your `loopers.yaml` configuration file.

Open `loopers.yaml` and add the `mcp:` section:

```yaml
# loopers.yaml
mcp:
  enabled: true
  servers:
    # Give your server a name and provide its URL
    - name: "filesystem"
      url: "http://localhost:3001"
    - name: "github"
      url: "http://localhost:3002"
      
  # The Circuit Breaker stops infinite tool loops automatically
  circuit_breaker:
    threshold: 5         # Stop if the exact same tool is called 5 times...
    window_seconds: 60   # ...within 60 seconds
```

## 2. Set Your Tool Costs

Not all tools cost the same. A simple filesystem read might be essentially free, but a massive Snowflake query could cost real money.

You can configure exact prices for specific tools in your `pricing.yaml` file:

```yaml
# pricing.yaml

tool_costs:
  defaults:
    unknown_tool: 0.001   # Default cost ($0.001) for any unmapped tool
    
  tools:
    "snowflake_query":
      cost_per_call: 0.05
    "github_api":
      cost_per_call: 0.001
    "web_search":
      cost_per_call: 0.01
```

> **Note:** Whenever an agent calls a tool, Loopers will deduct the specified `cost_per_call` from the agent's budget. If the budget runs out, the tool call is instantly blocked (fail-closed).

## 3. Generate a Budget Key

Before an agent can call an MCP server, it needs a budget key. You create this exactly like you would for an LLM!

Generate a key:
```bash
loopers keys create --name my-agent --provider mcp
```
*(This returns your raw proxy key, e.g., `lp-abc123`, and its hash)*

Assign a budget to the key:
```bash
loopers budget set <HASH> --hourly 1.00 --daily 5.00
```

## 4. Route Traffic Through Loopers

Loopers supports two primary integration topologies: **Remote HTTP MCP Routing** and **Local Stdio Process Wrapping**.

---

### Option A: Remote HTTP MCP Gateway

If your Loopers proxy is running on `http://localhost:8080`, point your HTTP-based agent client directly to:
`http://localhost:8080/mcp/<server-name>/`

#### Example Request

Send a `tools/call` JSON-RPC request through Loopers to the "github" MCP server:

```bash
curl -X POST http://localhost:8080/mcp/github/ \
  -H "Authorization: Bearer <RAW_LP_KEY>" \
  -H "X-Loopers-Session-ID: session-123" \
  -H "X-Loopers-Session-Max-Servers: 2" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "github_api",
      "arguments": {"repo": "loopers-oss", "action": "list_pulls"}
    }
  }'
```

---

### Option B: Local Stdio Process Wrapping (`loopers mcp wrap`)

Almost all local autonomous agents (e.g., **ZeroClaw**, **Claude Desktop**, **Cursor**) spawn local MCP servers via subprocess pipes using standard input/output (`stdio`) rather than HTTP.

To protect these tools with zero agent code changes, use `loopers mcp wrap`. Loopers acts as a transparent man-in-the-middle on the OS pipe, intercepting child process communication to minify schemas, offload bloated outputs into Ghost Tokens, and record live FinOps telemetry.

#### Syntax:
```bash
loopers mcp wrap --server <server_name> --key <lp-proxy-key> [--threshold <bytes>] -- <child_command...>
```

#### 1. ZeroClaw Integration (`config.toml`)
```toml
# In C:\Users\<user>\zeroclaw\config.toml or ~/.zeroclaw/config.toml

[[mcp.servers]]
name = "notion"
transport = "stdio"
command = "loopers"
args = ["mcp", "wrap", "--server", "notion", "--key", "lp-your-key", "--", "npx.cmd", "-y", "mcp-remote", "https://mcp.notion.com/mcp"]

[[mcp.servers]]
name = "fetch"
transport = "stdio"
command = "loopers"
args = ["mcp", "wrap", "--server", "fetch", "--key", "lp-your-key", "--", "python", "-m", "mcp_server_fetch"]
```
*(Ensure `loopers` and `loopers.exe` are in your `[risk_profiles.default] allowed_commands` list)*

#### 2. Claude Desktop Integration (`claude_desktop_config.json`)
```json
{
  "mcpServers": {
    "filesystem": {
      "command": "loopers",
      "args": ["mcp", "wrap", "--server", "filesystem", "--key", "lp-your-key", "--", "npx", "-y", "@modelcontextprotocol/server-filesystem", "/path/to/allowed/dir"]
    }
  }
}
```

---

### Option C: Stdio-to-HTTP Gateway Bridge (`loopers mcp bridge`)

If you run a centralized Loopers AI Firewall server on `:8080` and want a local stdio-only client to talk to an upstream HTTP MCP server:

```json
{
  "mcpServers": {
    "enterprise": {
      "command": "loopers",
      "args": ["mcp", "bridge", "--server", "enterprise", "--key", "lp-your-key", "--url", "http://127.0.0.1:8080"]
    }
  }
}
```

---

## 5. Tool Response Inspection (Indirect Prompt Injection Wall)

Loopers doesn't just inspect outgoing requests—it also inspects the **outputs/responses** returned by MCP tools in real-time. This protects your agent from two critical risks before a tool's response is returned to the client:

1. **Indirect Prompt Injection Defense (Transform):** If a tool retrieves external content (like reading a webpage or file) that contains malicious instructions designed to override agent directives (e.g., *"ignore previous instructions"*), Loopers sanitizes the text, replaces the content with `[Content removed: security policy]`, and injects the `X-Loopers-Response-Redacted: true` header.
2. **Secret Leakage Protection (Quarantine):** If a tool response leaks system credentials, API keys, or private keys (e.g., AWS, OpenAI, GitHub tokens, JWTs, PEM private key blocks), Loopers intercepts the payload, blocks the response with a self-correcting JSON-RPC error containing `X-Loopers-Policy-Block: true`, and **quarantines the agent's proxy key** in Redis for a configurable duration (default: `1h`), immediately blocking all subsequent requests.

### Configuration

Add the `inspector:` configuration block under the `mcp:` section in your `loopers.yaml` file:

```yaml
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      url: "http://localhost:3001"
  
  # Tool Response Inspection
  inspector:
    enabled: true
    quarantine_duration: "1h"
    custom_injection_patterns: []
    #  - "ignore system directive"
```

### What Happens Behind the Scenes?

1. **Schema Minifier (`tools/list`)**: When the agent requests the tool catalog, Loopers strips verbose markdown documentation, nested JSON Schema boilerplate, and redundant enums. This compresses discovery payloads (e.g. stripping 40,000+ tokens on Notion) while keeping 100% parameter validity for LLM tool calling.
2. **Ghost Token Offloading (`tools/call`)**: If a tool returns a massive payload (>4KB by default), Loopers encrypts the raw response (AES-256-GCM / HKDF) into Redis and replaces it on the wire with an ~80-token Ghost Stub, preventing quadratic context growth across subsequent turns.
3. **Read-Only Tool Memoization**: Repeated idempotent tool calls (e.g. read queries with identical arguments) are served directly from Redis in `<1ms` with `$0.00` upstream API spend.
4. **Blast Radius Tracking**: Loopers tracks active tool servers per session. If a session attempts to touch more distinct servers than authorized by policy or headers (`X-Loopers-Session-Max-Servers`), Loopers instantly blocks the lateral movement with `403 Forbidden`.
5. **Circuit Breaking**: If the agent gets stuck calling the exact same tool repeatedly within the sliding window, Loopers intercepts the call and returns a `429 Too Many Requests` or JSON-RPC error, breaking the loop.
6. **Budget Deduction**: Loopers checks `pricing.yaml` and atomically leases funds from the agent key's Redis budget.
7. **Response Inspection**: For `tools/call` response payloads, Loopers buffers the response and runs a stateless pattern scanner. If an indirect prompt injection is found, it transforms the body; if a secret leak is detected, it locks out the agent in Redis and returns a self-correcting error.

