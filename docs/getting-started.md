# Getting Started with Loopers

Welcome to Loopers! Loopers is an open-source AI firewall and FinOps gateway designed for autonomous agents. It eliminates prompt and tool schema bloat, offloads repetitive tool baggage via Ghost Tokens, and enforces real-time budget and security circuit breakers.

---

## 3-Second Zero-Config Audit (No Redis Required)

Before provisioning infrastructure, run a read-only audit directly in your agent workspace (OpenClaw, ZeroClaw, OpenCode, CrewAI, LangGraph, or custom MCP configs):

```bash
# Instant runner via npx (Node 18+):
npx @loopers/audit

# Or via diagnostic script:
curl -sSL https://tryloopers.com/audit.sh | sh

# Or directly if loopers is installed:
loopers audit
```

What the audit evaluates:
1. **Static Prompt & MCP Overhead:** Scans workspace prompt files (`SOUL.md`, `AGENTS.md`) and minifies MCP schemas.
2. **Dead "Ghost Tools":** Identifies MCP tools declared in schemas that were never called across recorded `.jsonl` sessions.
3. **Session Trajectory & Spend Split:** Analyzes multi-turn tool output baggage and computes input vs. output token spend ratio.
4. **Regime 1 Security & DLP:** Scans session logs for cleartext API keys, JWTs, unmasked private network IPs, and indirect prompt injection patterns.
5. **Scorecard Projections:** Compares per-turn token prefix savings and monthly costs across your active model and frontier models (Claude Sonnet 4.6).

To automatically prune unused tools and generate a tailored configuration:
```bash
loopers audit --fix
```

To run your agent immediately through an in-memory Loopers firewall with zero Redis setup:
```bash
loopers exec -- <your-agent-command>
```

---

## Prerequisites
- Go 1.22+ (if running/compiling from source)
- Redis 7.0+ (required for persistent daemon mode)
- Docker & Docker Compose (optional, but recommended for production)

## Installation

### Method 1: Using docker-compose (Recommended)
The easiest way to start Loopers is with our interactive setup wizard:

```bash
# Run the initialization wizard
go run github.com/try-loopers/loopers-oss/cmd/loopers init

# Start the proxy and Redis services
docker-compose up -d
```

### Method 2: From Source
```bash
# Clone the repository
git clone https://github.com/try-loopers/loopers-oss.git
cd loopers-oss

# Build the binary
go build -o loopers ./cmd/loopers

# Run the proxy server (bypassing TLS for local development)
SERVER_INSECURE_DEV=true ./loopers serve
```

## Quick Start Configuration

### 1. Create a Loopers Proxy Key
Create a new proxy key registered for a specific upstream provider (e.g., `openai`):

```bash
loopers keys create --name my-app-key --provider openai
```

This will output:
- **Raw Key**: Starting with `lp-...` (Copy this now! It will not be shown again).
- **Key Hash**: The SHA-256 hash of the raw key.

### 2. Configure a Budget for the Key
Set daily and hourly budgets for the key hash:

```bash
loopers budget set <key-hash> --daily 10.00 --hourly 2.00
```

### 3. Route Your LLM Requests through Loopers
Modify your application to target the Loopers endpoint instead of the direct provider API, passing your Loopers Key in the `Authorization` header and your raw provider key in `X-Loopers-Provider-Key`:

```python
from loopers_client import LoopersOpenAI

client = LoopersOpenAI(
    loopers_url="http://localhost:8080",
    loopers_key="lp-xxx",
    provider_key="sk-proj-xxx"
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello, Loopers!"}]
)

print(response.loopers_cost)
```
