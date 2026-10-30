---
id: getting-started
title: Getting Started
sidebar_label: Getting Started
description: Install and run Loopers to protect your budget in under one minute
---

# Getting Started with Loopers

**Loopers** is an open-source, baremetal **AI Firewall** engineered specifically for autonomous agent swarms and LLM applications. Built in native Go, Loopers acts as a transparent, sub-millisecond reverse proxy that governs LLM requests and Model Context Protocol (MCP) tool execution.

Loopers provides dual-regime protection across your entire AI infrastructure:

* **Regime 1: Rogue Agent Defense ("Bad Days" Protection)** — Active circuit breakers that instantly halt infinite agent loops, multi-turn objective drift, recursive MCP tool execution, data exfiltration, and budget blowouts.
* **Regime 2: Inline Context Acceleration & Wire Optimization ("Normal Days" Optimization)** — Slashes daily LLM production bills by **65% to 75%** on normal, healthy agent traffic through Ghost Token side-channel offload, deterministic KV-cache prefix canonicalization, and fast-tier routing with zero quality degradation.

This guide will help you get set up and protected in under five minutes.

## Prerequisites

Before starting, you will need:
* Go 1.25 or higher (required when building from source)
* Docker and Docker Compose (recommended for running Redis)

## Step 0: Instant Diagnostic (Zero Dependencies / Zero Setup)

Before deploying a proxy or configuring a Redis instance, you can run a read-only audit of your local agent workspace (OpenClaw, ZeroClaw, OpenCode, CrewAI, LangGraph, or custom MCP configs) to inspect prompt prefix bloat, measure compressible MCP schemas, replay `.jsonl` session histories, and score tool blast-radius risk:

```bash
# Instant zero-install diagnostic runner (macOS / Linux / WSL):
curl -sSL https://tryloopers.com/audit.sh | sh

# Or run via npx (Node 18+):
npx @loopers/audit

# Or run directly if you have loopers installed:
loopers audit
```

* **Instant Tailored Configuration:** Pass `--fix` (`loopers audit --fix`) to automatically generate a `loopers.yaml` pre-configured with all discovered FinOps & security controls.
* **Inline Protection Without Redis:** Run `loopers exec -- <your-agent-command>`. Loopers automatically boots an embedded, in-process memory store with an inline `$10.00` safety lease, delivering live Ghost Token and circuit-breaker protection on your very next turn with zero Redis configuration.

---

## Method 1: Docker (Recommended)

This is the fastest way to run Loopers in production or local development without installing Go.

### Step 1: Get the Docker Compose file

```bash
git clone https://github.com/try-loopers/loopers-oss.git
cd loopers-oss
```

### Step 2: Start Redis and the Proxy

Start both the proxy server and the Redis database in the background:

```bash
docker-compose up -d
```

### Step 3: Create a proxy key

Create a security key for your application. This key replaces your real AI provider key in your code:

```bash
docker-compose exec loopers /app/loopers keys create --name mykey --provider openai
```

Write down your raw key. It starts with lp and is only shown once. Copy it immediately.

### Step 4: Set a budget limit

Configure how much this key is allowed to spend:

```bash
docker-compose exec loopers /app/loopers budget set KEY_HASH --minute 0.50 --hourly 2.00 --daily 10.00 --weekly 50.00 --monthly 150.00
```

All spending limits are optional. The first limit you reach will block any further requests.

### Step 5: Route your first request

Point your application at the Loopers proxy:

```bash
curl -X POST http://localhost:8080/openai/v1/chat/completions \
  -H "Authorization: Bearer RAW_LP_KEY" \
  -H "X-Loopers-Provider-Key: YOUR_REAL_OPENAI_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "Hello Loopers"}]}'
```

*Alternatively, use path-based auth for agents (like `opencode` or `codex`) that don't support custom headers:*

**macOS / Linux:**
```bash
curl -X POST http://localhost:8080/RAW_LP_KEY/openai/v1/chat/completions \
  -H "Authorization: Bearer YOUR_REAL_OPENAI_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "Hello Loopers"}]}'
```

**Windows (PowerShell):**
```powershell
Invoke-RestMethod -Method Post -Uri "http://localhost:8080/RAW_LP_KEY/openai/v1/chat/completions" `
  -Headers @{"Authorization"="Bearer YOUR_REAL_OPENAI_KEY"; "Content-Type"="application/json"} `
  -Body '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "Hello Loopers"}]}'
```

---

## Method 2: Native Installation (Go 1.25+)

If you want to run the proxy natively on your machine or just use the CLI wrapper for agents (`loopers exec`).

### Step 1: Install the CLI globally

**macOS / Linux / Windows:**
```bash
go install github.com/try-loopers/loopers-oss/cmd/loopers@latest
```

### Step 2: Initialize Configuration

This creates a `loopers.yaml` configuration file and a `docker-compose.yml` (for Redis) in your current directory.

```bash
loopers init
```

### Step 3: Start Redis

Start only the Redis container in the background:

```bash
docker-compose up -d redis
```

> **Note:** If you already have Redis running locally (e.g. via Homebrew, apt, or a remote instance), you can skip this step. Just make sure the `redis.addr` in your `loopers.yaml` points to your existing Redis instance.

> [!WARNING]
> The default Redis password in the generated `docker-compose.yml` was recently updated from `changeme_in_production` to `demo-pass`. If you generated your compose file in an older version, make sure the `REDIS_PASSWORD` in `docker-compose.yml` matches `redis.password` in your `loopers.yaml`.

### Step 4: Start the Proxy Server

Start the server in development mode (without TLS):

**macOS / Linux:**
```bash
SERVER_INSECURE_DEV=true loopers serve
```

**Windows (PowerShell):**
```powershell
$env:SERVER_INSECURE_DEV="true"; loopers serve
```

### Step 5: Create a Key and Route Requests

In a **new terminal window**, you can now run Loopers commands directly against your local proxy:

```bash
loopers keys create --name mykey --provider openai
```

## Next Steps

* Read the [Architecture](./architecture) guide to understand how Loopers works
* Learn about [Budget Windows](./concepts/budget-windows) to fine tune your limits
* Connect your code with the [Python SDK](./sdks/python)
* Check the [CLI Reference](./reference/cli) for a list of all commands
