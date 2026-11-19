---
id: overview
title: Enterprise In-VPC Appliance Overview
sidebar_label: Overview & Architecture
description: Architecture, security boundaries, and hardware sizing for the Loopers Enterprise In-VPC Appliance.
---

# Enterprise In-VPC Appliance Overview

The **Loopers Enterprise Appliance** is a self-contained, air-gapped inference firewall and FinOps acceleration gateway designed to run entirely within your private cloud (AWS VPC, GCP VPC, Azure VNet, or on-premises Kubernetes).

It provides **100% Zero External SaaS Data Exfiltration**, autonomous identity governance, mid-stream spending enforcement, and cryptographic auditability for autonomous agent swarms.

---

## Architecture & Port Mapping

The appliance operates as a reverse proxy between your autonomous AI agents (running OpenAI, Anthropic, LangChain, CrewAI, or AutoGen SDKs) and your upstream LLM providers (Azure OpenAI, AWS Bedrock, Anthropic, or private vLLM / Ollama clusters):

```
                        ┌────────────────────────────────────────────────────────┐
                        │              YOUR VIRTUAL PRIVATE CLOUD                │
                        │                                                        │
[Autonomous AI Swarms] ─┼─► Port 8081: Loopers Data Plane (Engine)               │
  (OpenAI/Anthropic SDK)│     ├── Continuous Turn Router (Hybrid vLLM)           │
                        │     ├── Swarm Prompt Cache & Ghost Blobs               │
                        │     ├── Deep Packet Inspection (DPI) & DLP             │
                        │     └── Mid-Stream Budget Enforcer (SSE)               │
                        │                                                        │
[Enterprise Browser]   ─┼─► Port 3000: Web Console (apps/web)                    │
                        │     └── Fleet X-Ray, Anomaly Monitor, Chargeback       │
                        │                                                        │
[SecOps & CI/CD]       ─┼─► Port 8080: Control Plane API (loopers-api)           │
                        │     ├── SAML 2.0 / OIDC IdP Federation (Okta/Entra)    │
                        │     ├── Local AES-256 / HashiCorp Vault KMS Envelope   │
                        │     └── FOCUS 1.0 Department Chargeback Export         │
                        └────────────────────────────────────────────────────────┘
```

### Port Allocation:
| Port | Protocol | Component | Purpose |
| :--- | :--- | :--- | :--- |
| **`8081`** | HTTP/1.1, HTTP/2 | Data Plane Proxy<br/>`loopers-engine` | Target for all LLM inference traffic from agent fleets. |
| **`8080`** | HTTP/JSON | Control Plane API<br/>`loopers-api` | Administration, SAML metadata, and secret vaults. |
| **`3000`** | HTTP | Web Console<br/>`apps/web` | Dashboard for FinOps, audit receipts, and agent keys. |
| `5432` | TCP (Internal) | PostgreSQL 16 | Relational state store for projects, keys, and receipts. |
| `6379` | TCP (Internal) | Redis 7 | Token buckets, session prompt caches, and loop breakers. |
| `4222` | TCP (Internal) | NATS JetStream | Event bus for real-time telemetry and auditing. |

---

## Prerequisites & Hardware Sizing

* **Container Runtime:** Docker 24.0+ & Docker Compose v2+ (or Kubernetes 1.25+ with Helm 3.10+).
* **OS / Container ulimits:** Container file descriptor limit (`nofile`) must be set to `65535` (the Linux default of 1,024 drops sockets above ~500 concurrent connections).
* **Compute per Engine Pod:**
  * **Requests:** 1 vCPU, 1 GiB RAM
  * **Limits:** 4 vCPU, 4 GiB RAM
* **Persistent Storage:** 50 GB SSD persistent volume for PostgreSQL and Redis append-only files.

### Empirical Pod Capacity Profile (Streaming Agents):
Benchmarks conducted with synthetic multi-agent streaming workloads (~25 tokens/sec per stream):
* **0 – 800 Concurrent Streams per Pod (Production Sweet Spot):** P50 latency < 2.5s, P95 < 3.5s, 0% queuing delay.
* **1,000 – 1,200 Streams per Pod (Autoscaling Inflection Point):** Worker saturation begins; connection queue latency increases.
* **1,500+ Streams per Pod (Congested):** P95 latency exceeds 6.0s; horizontal pod autoscaling (HPA) required.
* **3,500 Streams per Pod (Single-Pod Hardware Ceiling):** 100% request completion maintained with only 250 MB RSS memory.
