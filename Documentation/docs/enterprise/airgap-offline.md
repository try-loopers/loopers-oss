---
id: airgap-offline
title: Strict Air-Gapped & Offline VPC Deployment
sidebar_label: Setup C - Air-Gapped / Offline
description: Guide for deploying in strict air-gapped VPCs with zero outbound internet access.
---

# Strict Air-Gapped & Offline VPC Deployment

For high-security enclaves (defense, national intelligence, financial core banking, or government clouds) that operate with **zero outbound internet access**, Loopers Cloud can be pre-packaged into a single-file offline container bundle.

---

## 1. Export Image Bundle on a Connected Build Machine

On an internet-connected workstation or CI/CD runner:

```bash
# From the root of the loopers repository / package:
chmod +x ./deploy/scripts/export-bundle.sh
./deploy/scripts/export-bundle.sh
```

*What this produces:*
A single compressed archive at `deploy/loopers-appliance-images.tar.gz` containing:
- `postgres:16-alpine`
- `redis:7-alpine`
- `nats:latest`
- `loopers-cloud-api:latest`
- `loopers-cloud-engine:latest`
- `loopers-cloud-compliance:latest`
- `loopers-cloud-web:latest`

---

## 2. Transfer to Air-Gapped Environment

Transfer the following artifacts across your secure data diode, bastion, or physical media:
1. `loopers-cloud-enterprise-*.zip` (Source code, compose manifests, scripts)
2. `SHA256SUMS.txt` (Cryptographic verification checksums)
3. `deploy/loopers-appliance-images.tar.gz` (Pre-built container bundle)
4. `license.jwt` (Your vendor-issued enterprise license token)

---

## 3. Load and Launch Inside the Air-Gapped Host

Inside your isolated VPC host:

```bash
# 1. Verify archive integrity
sha256sum -c SHA256SUMS.txt

# 2. Unpack the application package
unzip loopers-cloud-enterprise-*.zip
cd loopers-cloud-enterprise-*

# 3. Import the offline container image bundle
docker load -i deploy/loopers-appliance-images.tar.gz

# 4. Initialize cryptographic secrets
./deploy/scripts/init-env.sh

# 5. Place your enterprise license token (MANDATORY)
cp /path/to/license.jwt deploy/license.jwt

# 6. Launch the appliance (using imported images)
docker compose -f deploy/docker-compose.in-vpc.yml up -d

# 7. Verify appliance readiness (zero external dependencies)
docker compose -f deploy/docker-compose.in-vpc.yml ps
curl -s -f http://localhost:8080/readyz
curl -s -f http://localhost:8081/readyz
curl -s -f http://localhost:3000/api/health
```

*(Optional Developer Suite: run `./deploy/scripts/smoke-test.sh` if Go 1.26+ is installed on the host)*

The appliance will boot with 100% of telemetry, semantic caching, and threat inspection running locally against internal PostgreSQL, Redis, and NATS.
