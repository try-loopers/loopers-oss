---
id: sso-saml
title: Enterprise SSO Federation (SAML 2.0 & OIDC)
sidebar_label: SSO & SAML 2.0
description: Integrate Okta, Microsoft Entra ID (Azure AD), Google Workspace, or Ping Identity via SAML 2.0.
---

# Enterprise SSO Federation (SAML 2.0 & OIDC)

Loopers Cloud includes an embedded, high-performance SAML 2.0 Service Provider (SP) and OpenID Connect (OIDC) client that seamlessly connects to your corporate Identity Provider (IdP).

---

## 1. Obtain Service Provider (SP) Metadata

The appliance publishes its SAML XML metadata directly from the control-plane API:

```text
http://<appliance-ip>:8080/v1/auth/sso/metadata
```

Download this XML file or copy the endpoint URL into your Okta or Microsoft Entra ID tenant configuration.

---

## 2. Identity Provider (IdP) Configuration

### Core SAML Attributes:
* **Assertion Consumer Service (ACS) URL:**  
  `http://<appliance-ip>:8080/v1/auth/sso/callback`
* **Entity ID / Audience URI:**  
  `https://loopers.corp.internal/v1/auth/sso/metadata`
* **NameID Format:**  
  `EmailAddress` (`urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress`)

### Attribute Statements:
Ensure your IdP maps the following SAML assertion attributes:
* `email` $\rightarrow$ `user.email`
* `firstName` $\rightarrow$ `user.firstName`
* `lastName` $\rightarrow$ `user.lastName`
* `groups` $\rightarrow$ `user.groups` (or Active Directory security groups)

---

## 3. Automatic RBAC Role Mapping

Configure AD/Okta groups to map directly to Loopers roles in the Web Console under **Settings $\rightarrow$ SSO**:

| Corporate Directory Group | Loopers Appliance Role | Granted Permissions |
| :--- | :--- | :--- |
| **`Domain Admins`** | `ADMIN` | Organization settings, KMS envelope keys, SSO config, user deletion. |
| **`SecOps-Auditors`** | `SECURITY` | Threat incident response, forensic receipts, and emergency kill switches. |
| **`AI-Platform-Engineers`** | `DEVELOPER` | Agent key generation (`lc_sk_...`), project budgets, and telemetry views. |
| **`All-Employees`** | `VIEWER` | Read-only dashboards and audit receipts. |
