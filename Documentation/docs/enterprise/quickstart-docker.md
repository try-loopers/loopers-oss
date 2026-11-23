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

## 5-Stage Step-by-Step Deployment

### Stage 0: Obtain the Appliance Package

Choose the method that matches your network environment:

#### Option A: Direct Download (Standard Cloud & Local Evaluation)
Create a dedicated installation directory and download the official release assets:

```bash
# On Linux / macOS:
mkdir -p loopers-appliance && cd loopers-appliance
curl -LO https://github.com/CURSED-ME/loopers-cloud/releases/download/v1.0.0/loopers-cloud-enterprise-v1.0.0.zip
curl -LO https://github.com/CURSED-ME/loopers-cloud/releases/download/v1.0.0/SHA256SUMS.txt
```

```powershell
# On Windows PowerShell:
New-Item -ItemType Directory -Path "loopers-appliance" -Force | Out-Null
Set-Location "loopers-appliance"
Invoke-WebRequest -Uri "https://github.com/CURSED-ME/loopers-cloud/releases/download/v1.0.0/loopers-cloud-enterprise-v1.0.0.zip" -OutFile "loopers-cloud-enterprise-v1.0.0.zip"
Invoke-WebRequest -Uri "https://github.com/CURSED-ME/loopers-cloud/releases/download/v1.0.0/SHA256SUMS.txt" -OutFile "SHA256SUMS.txt"
```

#### Option B: Air-Gapped / Pre-Staged Transfer (Enterprise Production VPC)
In strict enclaves with zero outbound internet egress:
1. Download `loopers-cloud-enterprise-v1.0.0.zip` and `SHA256SUMS.txt` on a connected workstation from your **Customer Portal** or internal artifact repository (JFrog Artifactory, AWS S3 bucket).
2. Transfer the archive across your secure bastion, jump box, or physical media into your staging directory.

---

### Stage 1: Verify Release Integrity & Unpack

Verify the cryptographic SHA-256 checksum to guarantee that the package has not been corrupted or tampered with in transit:

```bash
# On Linux / macOS:
sha256sum -c SHA256SUMS.txt
unzip loopers-cloud-enterprise-v1.0.0.zip
cd loopers-cloud-enterprise-v1.0.0
```

```powershell
# On Windows PowerShell:
$expected = ((Get-Content .\SHA256SUMS.txt) -split '\s+')[0].Trim().ToLower()
$actual = (Get-FileHash .\loopers-cloud-enterprise-v1.0.0.zip -Algorithm SHA256).Hash.ToLower()
if ($actual -ne $expected) { throw "Checksum mismatch! Abort deployment." }
Expand-Archive .\loopers-cloud-enterprise-v1.0.0.zip -DestinationPath .
```

---

### Stage 2: Initialize Cryptographic Secrets
From the root of your unzipped appliance directory:

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

---

### Stage 3: Place Your Enterprise License Token (Mandatory)
The engine proxy requires a vendor-signed enterprise license file at `deploy/license.jwt` before booting.

```bash
# If you have your license file:
cp /path/to/customer-license.jwt deploy/license.jwt

# Or write your license token string:
echo "lc_lic_YOUR_TOKEN_HERE" > deploy/license.jwt
```

```powershell
# On Windows PowerShell:
"lc_lic_YOUR_TOKEN_HERE" | Set-Content deploy\license.jwt -NoNewline
```

> [!IMPORTANT]
> The engine terminates with `[License] FATAL` if `deploy/license.jwt` is missing or empty. If you do not yet have an enterprise license token, request an evaluation key via `sales@tryloopers.com` or your account representative.

---

### Stage 4: Build and Launch the Appliance Stack
Run Docker Compose with the `--build` flag to compile the multi-stage Dockerfiles locally:

```bash
# On Linux / macOS:
docker compose -f deploy/docker-compose.in-vpc.yml up --build -d

# On Windows PowerShell:
docker compose -f deploy\docker-compose.in-vpc.yml up --build -d
```

Wait ~30 seconds for containers to initialize, then verify all 6 services are healthy:
```bash
docker compose -f deploy/docker-compose.in-vpc.yml ps
```

Validate container readiness probes:
```bash
# Control Plane API (verifies Postgres + Redis):
curl -s -f http://localhost:8080/readyz

# Data Plane Firewall (verifies Engine active & license verified):
curl -s -f http://localhost:8081/readyz

# Web Console:
curl -s -f http://localhost:3000/api/health
```

---

### Stage 5: First Login & Live Agent Traffic Test

1. **Retrieve Root Admin Credentials:**
   ```bash
   # Linux / macOS:
   grep -E 'INITIAL_ADMIN_EMAIL|INITIAL_ADMIN_PASSWORD' .env

   # Windows PowerShell:
   Get-Content .env | Select-String 'INITIAL_ADMIN_'
   ```
2. **Access the Web Console:**
   Open `http://localhost:3000` in your browser and log in with the administrator credentials above.
3. **Issue an Agent Key:**
   Navigate to **Agent Keys** &rarr; **Create Key** (e.g., `prod-agent-key`). Copy the generated `lc_sk_...` key.
4. **Send a Test Request Through the Data Plane Firewall (`:8081`):**
   ```bash
   curl -X POST http://localhost:8081/v1/chat/completions \
     -H "Authorization: Bearer lc_sk_your_key_here" \
     -H "Content-Type: application/json" \
     -d '{
       "model": "gpt-4o",
       "messages": [{"role": "user", "content": "Hello Loopers Firewall"}]
     }'
   ```
5. **View Cryptographic Audit Trail:**
   Open the Web Console dashboard (`http://localhost:3000`) to inspect the real-time forensic receipt, mid-stream budget enforcement, and Deep Packet Inspection (DLP) telemetry.

---

## Autonomous AI Coding Assistant Deployment

If you are using an AI coding assistant (Cursor, Claude Code, Antigravity, GitHub Copilot CLI, or Aider):

1. Unpack the appliance into your workspace.
2. Prompt your assistant:
   > *"Deploy this appliance following AGENTS.md. My enterprise license token is `<token>`."*
3. The assistant will autonomously execute the pre-flight checks, generate secrets, insert the license, launch containers, and verify `/readyz` endpoints.

To run the full internal unit and static verification suite (requires Go 1.26+ on host):

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
