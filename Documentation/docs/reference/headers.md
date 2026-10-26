---
id: headers
title: API Headers Reference
sidebar_label: API Headers
description: All HTTP request and response headers used by the Loopers proxy.
---

# API Headers Reference

Loopers uses headers to get settings from your application and return information about each request. You can think of headers as stickers on a package that give instructions or show details.

## Request Headers

| Header | Required | Description |
|---|---|---|
| Authorization: Bearer `<KEY>` | Yes | Your Loopers proxy key, OR your real upstream provider key if using Zero-Code Path Integration. |
| X-Loopers-Provider-Key | Yes* | Your real AI provider API key (*Not required if using Zero-Code Path Integration) |
| X-Loopers-Signature | No | The cryptographic signature injected by Loopers into the request forwarded to the upstream provider (when signature policy is enabled). |
| X-Loopers-Session-ID | No | Unique name for this session (for loop detection) |
| X-Loopers-Session-Budget | No | Maximum spending limit in USD for this session |
| X-Loopers-Session-Max-Steps | No | Maximum AI calls allowed for this session |
| X-Loopers-Skip-Hydration | No | Skip prompt tail-hydration of Ghost Tokens on intermediate turns |
| X-Loopers-Force-Frontier | No | Bypass Smart FinOps tier routing and force execution on the frontier model |
| Content-Type: application/json | Yes | Required for all POST requests |

## Response Headers

:::note
All `X-Loopers-*` budget and telemetry response headers can be completely suppressed by setting `server.strip_budget_headers: true` in your configuration (`loopers.yaml`).
:::

| Header | Description |
|---|---|
| X-Loopers-Request-Cost | The real cost of this request in USD |
| X-Loopers-Session-Spend | The total cost spent in this session so far |
| X-Loopers-Session-Steps | The number of AI calls made in this session so far |
| X-Loopers-Session-Remaining | The remaining budget left in your most limited window |
| X-Loopers-Budget-Window | The budget window that is closest to its limit (e.g., daily) |
| X-Loopers-Request-ID | A unique ID for this request to help with debugging |
| X-Loopers-Policy-Block | Set to `"true"` when a request or MCP tool call is blocked by OPA policy |
| X-Loopers-Block-Reason | Contains the denial reason rule text from the matching Rego policy |
| X-Loopers-Signature | Injected back into the HTTP response headers to return the cryptographic action receipt to the downstream client. |
| X-Loopers-FinOps-Route | Indicates routing tier (e.g., `"fast-tier"`) when Smart FinOps model routing is active. |
| X-Loopers-FinOps-Throttled | Set to `"true"` when output tokens were bounded on simple intermediate verification turns. |
| X-Loopers-Saved-USD | Cumulative USD saved on the request via arbitrage, ghost tokens, or memoized tools |
| X-Loopers-Ghost-Token | Blob ID emitted when an outbound MCP tool result is offloaded to a ghost stub |
| X-Loopers-MCP-Memoized | Set to `"true"` when an idempotent tool call was served directly from cache |


## Error Codes

| HTTP Status | JSON Type | Reason |
|---|---|---|
| 401 Unauthorized | invalid_key | The Loopers key is invalid or has been revoked |
| 403 Forbidden | policy_denied | The request was blocked by an OPA policy (LLM proxy calls) |
| 200 OK (MCP JSON-RPC `-32001`) | policy_denied | An MCP tool call was denied by OPA policy (enables LLM self-correction) |
| 429 Too Many Requests | budget_exceeded | You have reached your budget window limit |
| 429 Too Many Requests | loop_detected | An agent loop was detected in this session |
| 429 Too Many Requests | max_steps_exceeded | You have reached the maximum allowed calls for this session |
| 503 Service Unavailable | redis_unavailable | Redis is offline and requests are blocked for safety |

## Example Request

```bash
curl -X POST http://localhost:8080/openai/v1/chat/completions \
  -H "Authorization: Bearer lp-a1b2c3d4" \
  -H "X-Loopers-Provider-Key: sk-proj-..." \
  -H "X-Loopers-Session-ID: run-42" \
  -H "X-Loopers-Session-Budget: 5.00" \
  -H "X-Loopers-Session-Max-Steps: 10" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "Hi"}]}'
```

## Example Error Response

When your budget is exceeded, Loopers returns a response like this:

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json

{
  "error": "budget exceeded",
  "type": "budget_exceeded"
}
```
