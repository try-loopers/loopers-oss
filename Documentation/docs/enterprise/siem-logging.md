---
id: siem-logging
title: SIEM Integration & Forensic Logging
sidebar_label: SIEM & Forensic Logging
description: Real-time telemetry export to Splunk, Elastic, Datadog, Syslog, or air-gapped forensic log sinks.
---

# SIEM Integration & Forensic Logging

The `loopers-compliance` microservice monitors all NATS JetStream event subjects (`telemetry.receipts`, `threat.blocked`, `anomaly.flagged`) and exports cryptographically verified audit records to your enterprise SIEM.

---

## 1. Supported Platforms & Formats

| Platform Type | Supported Formats | Transport Security |
| :--- | :--- | :--- |
| **Splunk HEC** | JSON, CEF | HTTPS with token authentication and SSRF firewall verification. |
| **Elasticsearch / OpenSearch** | JSON | Index-per-day or unified `loopers-security` index via HTTPS. |
| **Syslog (TCP / TLS / UDP)** | RFC 5424, CEF, LEEF | TLS 1.3 / TCP / UDP with appliance-level SSRF dialing checks. |
| **Air-Gapped Forensic Log** | JSON Lines | Sandboxed strictly to `/var/log/loopers/forensic.log`. |

---

## 2. Disk WAL & Spool Resilience

When your corporate SIEM endpoint is temporarily unreachable (e.g., during network maintenance), the compliance service buffers failed events to an on-disk Write-Ahead Log (WAL) located at `/var/log/loopers/wal/`.

- **Retries:** Failed events retry with exponential backoff up to 3 attempts.
- **Persistence:** Events failing all retries are spooled to encrypted disk blocks.
- **Drain Worker:** When the SIEM endpoint recovers, a background worker drains and dispatches the spooled backlog in chronological order with zero event loss.

---

## 3. Configuring SIEM via Web Console

1. Navigate to `http://<appliance-ip>:3000`.
2. Open **Settings → SIEM & Compliance**.
3. Select your platform (e.g. `Splunk HEC` or `Syslog TLS`).
4. Enter your endpoint URL, authentication token, and format preference (`CEF` or `JSON`).
5. Click **Test SIEM Connection** to send an active probe and verify delivery.
