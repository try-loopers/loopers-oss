---
id: quickstart-docker
title: In-VPC Docker Compose Deployment
sidebar_label: Setup A - Docker Compose
description: Deploy the complete Loopers Enterprise stack in your VPC in 4 commands using Docker Compose.
---

# In-VPC Docker Compose Deployment

The Docker Compose stack is the fastest path to evaluating or operating Loopers Cloud Enterprise in a single virtual machine (AWS EC2, Azure VM, GCP Compute Engine, or on-premises server).

It builds all microservices locally from the included multi-stage Dockerfiles, requiring **zero container registry credentials** and zero public internet access.

---

## 4-Step Quickstart

### Step 1: Initialize Cryptographic Secrets
From the root of your unzipped `loopers-cloud-enterprise-rc1` directory:

```bash
# On Linux / macOS:
chmod +x deploy/scripts/init-env.sh
./deploy/scripts/init-env.sh

# On Windows PowerShell:
powershell -ExecutionPolicy Bypass -File .\deploy\scripts\init-env.ps1
```

*What this does:*
- Auto-generates cryptographically random internal HMAC secrets, PostgreSQL credentials, and administrator passwords.
- Creates `deploy/vault.key` (256-bit AES master encryption key).
- Generates `deploy/jwt.key` (2048-bit RSA private signing key).
- Pre-creates a safe `deploy/license.jwt` placeholder file.

---

### Step 2: Place Your Enterprise License
Copy the Ed25519-signed license token provided by your Loopers account team:

```bash
cp /path/to/customer-license.jwt deploy/license.jwt
```

*(If you are deploying an initial evaluation without a license file, the stack boots into evaluation mode; you can also paste the license token later via the Web Console).*

---

### Step 3: Build and Launch the Appliance Stack
Run Docker Compose with the `--build` flag to compile from local source:

```bash
docker compose -f deploy/docker-compose.in-vpc.yml up --build -d
```

Verify that all 7 containers are healthy:
```bash
docker compose -f deploy/docker-compose.in-vpc.yml ps
```

---

### Step 4: Run the Automated Smoke Test
Validate all 5 appliance security and architecture gates:

```bash
# On Linux / macOS:
./deploy/scripts/smoke-test.sh

# On Windows PowerShell:
powershell -ExecutionPolicy Bypass -File .\deploy\scripts\smoke-test.ps1
```

*Expected Output:*
```text
==========================================================
 LOOPERS ENTERPRISE IN-VPC APPLIANCE SMOKE TEST SUITE
==========================================================
[*] Verifying Appliance License (pkg/license)... PASS
[*] Running Enterprise In-VPC Audit (cmd/verify_enterprise)... PASS
[*] Running Security Blast Radius Linter (cmd/loopers-enterprise-audit)... PASS
[*] Validating In-VPC Docker Compose Manifest... PASS
[*] Verifying Core Test Suites (engine, api, billing, pkg)... PASS
==========================================================
 SMOKE TEST SCORECARD: 5 PASSED, 0 FAILED
==========================================================
[OK] Appliance is ready for partner deployment!
```

---

## Accessing the Appliance

- **Enterprise Web Console:** `http://<host-ip>:3000`
- **Data Plane (Target for Agent SDKs):** `http://<host-ip>:8081/v1`
- **Control Plane API:** `http://<host-ip>:8080/v1`
- **Initial Admin Credentials:** Look in your generated `.env` file for `INITIAL_ADMIN_EMAIL` and `INITIAL_ADMIN_PASSWORD`.
