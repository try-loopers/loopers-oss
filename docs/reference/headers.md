# Header Reference

Loopers utilizes specific custom HTTP headers to pass routing authentication, session configuration, and telemetry data.

## Request Headers

| Header | Description | Required | Example |
|---|---|---|---|
| `Authorization` | Bearer token containing the Loopers proxy key | Yes | `Bearer lp-openai-key-xxx` |
| `X-Loopers-Provider-Key` | Upstream provider API key (e.g. OpenAI or Anthropic key) | Yes* | `sk-proj-xxxxxx` |
| `X-Loopers-Signature` | Cryptographic signature injected into forwarded upstream requests | No | `sig=...` |
| `X-Loopers-Session-ID` | Unique session ID to enforce agent-level budgets and loop detection | No | `agent-run-103` |
| `X-Loopers-Session-Budget` | Dollar cap (USD) for the given agent session | No | `2.50` |
| `X-Loopers-Session-Max-Steps` | Step limit (max requests) for the given agent session | No | `20` |
| `X-Loopers-Skip-Hydration` | Skip prompt tail-hydration of Ghost Tokens on intermediate turns | No | `true` |
| `X-Loopers-Force-Frontier` | Bypass Smart FinOps tier routing and force frontier execution | No | `true` |

\* Not required when using Zero-Code Path Integration.

## Response Headers

> [!NOTE]
> All `X-Loopers-*` budget and telemetry response headers can be completely suppressed by setting `server.strip_budget_headers: true` in your configuration (`loopers.yaml`).

| Header | Description | Type | Example |
|---|---|---|---|
| `X-Loopers-Request-Cost-Estimated` | Calculated upper-bound estimated cost of the request | Float | `0.024500` |
| `X-Loopers-Request-Cost` | Actual calculated cost after token usage is reconciled | Float | `0.008400` |
| `X-Loopers-Session-Spend` | Total spend consumed by the session so far | Float | `0.125000` |
| `X-Loopers-Session-Steps` | Total steps executed by the session so far | Integer | `5` |
| `X-Loopers-Session-Remaining` | Remaining dollar budget (USD) left for the session | Float | `2.375000` |
| `X-Loopers-Saved-USD` | Cumulative USD saved on the request via arbitrage, ghost tokens, or memoized tools | Float | `0.015200` |
| `X-Loopers-Ghost-Token` | Blob ID emitted when an outbound MCP tool result is offloaded to a ghost stub | String | `7f83b1657ff1fc53` |
| `X-Loopers-MCP-Memoized` | Set to `"true"` when an idempotent tool call was served directly from cache | Boolean | `true` |
| `X-Loopers-FinOps-Route` | Indicates routing tier (e.g. `"fast-tier"` or `"frontier"`) | String | `fast-tier` |
| `X-Loopers-FinOps-Throttled` | Set to `"true"` when output tokens were bounded on simple verification turns | Boolean | `true` |
| `X-Loopers-Policy-Block` | Set to `"true"` when a request or MCP tool call is blocked by OPA policy | Boolean | `true` |
| `X-Loopers-Block-Reason` | Contains the denial reason rule text from the matching Rego policy | String | `Unauthorized tool` |
| `X-Loopers-Signature` | Cryptographic action receipt returned to downstream client | String | `sig=...` |
