---
id: licensing
title: Enterprise Cryptographic Licensing
sidebar_label: Licensing & Activation
description: How offline Ed25519 enterprise license tokens work and how to activate them.
---

# Enterprise Cryptographic Licensing

Loopers Cloud Enterprise uses an **offline, cryptographically signed Ed25519 license token format**.

The license requires **zero external license servers or internet phone-home calls**. The appliance data-plane and control-plane verify token signatures locally against the embedded Loopers Authority Public Key.

---

## 1. What a License Controls

Each `license.jwt` encapsulates:
- **`org_id` & `org_name`:** Licensed enterprise tenant identifier.
- **`cluster_name`:** Bound appliance cluster identifier.
- **`max_workers`:** Maximum concurrent autonomous agents / active streams.
- **`max_qps`:** Aggregate requests-per-second throughput ceiling.
- **`expires_at`:** Unix timestamp of license expiration.
- **`is_air_gapped`:** Strict air-gapped flag (suppresses outbound dynamic model polling).

---

## 2. Verifying a License via CLI

The package includes a standalone, read-only license verification tool:

```bash
go run ./cmd/license-gen -verify deploy/license.jwt
```

*Sample Output:*
```text
[PASSED] License is cryptographically valid!
  Org:         Primary Enterprise (org_primary_enterprise)
  Cluster:     on-prem-eval-cluster
  Tier:        enterprise
  Max Agents:  5000
  Max QPS:     10000
  Expires:     2027-11-19T00:00:00Z
  Air-Gapped:  false
```

---

## 3. Activating a License

There are three ways to supply a license to the appliance:

### Option A: File Placement (Recommended for In-VPC)
Drop the token into `deploy/license.jwt` before starting the containers:
```bash
cp customer-license.jwt deploy/license.jwt
```

### Option B: Web Console Activation
1. Navigate to `http://<appliance-ip>:3000`.
2. Go to **Settings $\rightarrow$ License**.
3. Paste the raw JWT token string into the activation field and click **Activate License**.

### Option C: Control Plane API
Submit the token directly to the control plane API endpoint:
```bash
curl -X POST http://localhost:8080/v1/enterprise/license/activate \
  -H "Authorization: Bearer <admin-session-token>" \
  -H "Content-Type: application/json" \
  -d '{"token":"<raw-jwt-token-string>"}'
```
