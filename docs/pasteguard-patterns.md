# PasteGuard Secret & PII Detection Patterns

> Reference for all detectable entity types, their regex patterns, and sources.

---

## Secret Entities (regex-based, in `privacy/secrets/`)

### Private Keys

| Entity | Pattern | FP Risk | Source |
|--------|---------|---------|--------|
| `OPENSSH_PRIVATE_KEY` | `-----BEGIN OPENSSH PRIVATE KEY-----[\s\S]*?-----END OPENSSH PRIVATE KEY-----` | Near zero | Gitleaks, TruffleHog |
| `PEM_PRIVATE_KEY` | `-----BEGIN (RSA\|ENCRYPTED\|) PRIVATE KEY-----[\s\S]*?-----END ... PRIVATE KEY-----` | Near zero | Gitleaks, TruffleHog |

### API Keys (prefixed, high confidence)

| Entity | Pattern | FP Risk | Source |
|--------|---------|---------|--------|
| `API_KEY_SK` | `sk[-_][a-zA-Z0-9_-]{20,}` | Low | Gitleaks `generic-api-key` |
| `API_KEY_AWS` | `AKIA[0-9A-Z]{16}` | Near zero | Gitleaks `aws-access-token` |
| `API_KEY_GITHUB` | `gh[pousr]_[a-zA-Z0-9]{36,}` | Near zero | Gitleaks `github-pat` |
| `API_KEY_GITLAB` | `gl(?:pat\|dt\|cbt\|ptt)-[a-zA-Z0-9_-]{20,}` | Near zero | Gitleaks `gitlab-ptt` |
| `API_KEY_GCP` | `AIza[0-9A-Za-z_-]{35}` | Near zero | Gitleaks `google-api-key` |
| `API_KEY_TENCENT` | `AKID[A-Za-z0-9]{32}` | Near zero | TruffleHog [#4036](https://github.com/trufflesecurity/trufflehog/issues/4036) |
| `API_KEY_ALIBABA` | `LTAI[A-Za-z0-9]{12,20}` | Near zero | TruffleHog `alibaba-access-key` |
| `API_KEY_SLACK` | `xox[bapors]-[a-zA-Z0-9-]{10,}` | Near zero | Gitleaks `slack-token` |
| `API_KEY_STRIPE` | `(?:sk\|rk)_live_[a-zA-Z0-9]{24,}` | Near zero | Gitleaks `stripe-secret-key` |
| `API_KEY_SENDGRID` | `SG\.[a-zA-Z0-9_-]{22}\.[a-zA-Z0-9_-]{43}` | Near zero | Gitleaks `sendgrid-api-key` |

### Tokens

| Entity | Pattern | FP Risk | Source |
|--------|---------|---------|--------|
| `JWT_TOKEN` | `eyJ[a-zA-Z0-9_-]{20,}\.eyJ[a-zA-Z0-9_-]{20,}\.[a-zA-Z0-9_-]{20,}` | Low | detect-secrets `KeywordDetector` |
| `BEARER_TOKEN` | `(?i)Bearer\s+[a-zA-Z0-9._-]{40,}` | Low | Gitleaks `bearer-token` |

### Context-Aware Variable Assignments

These match **variable name + value** patterns. They catch leaked credentials in Python dicts, YAML, env files, and source code.

| Entity | Pattern | Catches | Min Length |
|--------|---------|---------|------------|
| `ENV_PASSWORD` | `(?i)[A-Za-z_][A-Za-z0-9_]*(?:PASSWORD\|PASSWD\|_PWD\|_PASS)\s*[=:]\s*['"]?[^\s'"]{8,}['"]?` | `DB_PASSWORD=...`, `SMTP_PASS = "..."` | 8 chars |
| `ENV_SECRET` | `(?i)[A-Za-z_][A-Za-z0-9_]*_SECRET\s*[=:]\s*['"]?[^\s'"]{8,}['"]?` | `JWT_SECRET=...`, `CS_CLIENT_SECRET = "..."` | 8 chars |
| `ENV_TOKEN` | `(?i)(?:\btoken\b\|[A-Za-z_][A-Za-z0-9_]*_TOKEN)['"]?\s*[=:]\s*['"][a-zA-Z0-9_.+/=-]{16,}['"]` | `"token": "Vaka..."`, `API_TOKEN = "..."` | 16 chars |
| `ENV_CREDENTIAL` | `(?i)[A-Za-z_][A-Za-z0-9_]*(?:CLIENT_ID\|_CID\|_ACCESS_KEY)['"]?\s*[=:]\s*['"][a-zA-Z0-9_.-]{10,}['"]` | `CS_CLIENT_ID = "..."`, `AWS_ACCESS_KEY = "..."` | 10 chars |
| `CONNECTION_STRING` | `(?i)(?:postgres(?:ql)?\|mysql\|mariadb\|mongodb(?:\+srv)?\|redis\|amqps?):\/\/[^:]+:[^@\s]+@[^\s'"]+` | `postgres://user:pass@host/db` | N/A |
| `BASIC_AUTH_URL` | `(?i)[a-z][a-z0-9+\-.]*://[^\s:/'"]{2,}:[^\s@/'"]{4,}@[^\s'"]+` | `https://admin:pass@host` | 4 chars (password) |
| `VAULT_TOKEN` | `hvs\.[a-zA-Z0-9_-]{24,}` | `hvs.aaaa...` | 24 chars |
| `AZURE_CREDENTIAL` | `(?i)(?:AZURE\|TENANT\|AAD)*(?:SECRET\|KEY\|PASSWORD\|TOKEN\|ID)\s*[=:]\s*['"][a-f0-9]{8}-...-[a-f0-9]{12}['"]` | `AZURE_CLIENT_SECRET="uuid"` | UUID format |

**False-positive controls:**
- `ENV_TOKEN` uses `\btoken\b` (word boundary) to avoid matching `token_type`, `next_token` as dict keys
- `ENV_TOKEN` requires 16+ char values to skip `"token": "Bearer"`, `"token": "access"`
- `ENV_PASSWORD` requires 8+ char values to skip `PASS="abc"`

### Local PII (secrets layer)

| Entity | Pattern | FP Risk |
|--------|---------|---------|
| `THAI_NATIONAL_ID` | `\b[1-8]\d{12}\b` | Low (digits only, starts 1-8) |

---

## PII Entities (regex-based, in `privacy/pii/`)

| Entity | Pattern | Score | FP Controls |
|--------|---------|-------|-------------|
| `EMAIL_ADDRESS` | `[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}` | 0.95 | None |
| `PHONE_NUMBER` | `(?:\+?\d{1,3}[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}` | 0.90 | Skip if inside URL |
| `CREDIT_CARD` | `\b(?:4\d{3}\|5[1-5]\d{2}\|2[2-7]\d{2}\|3[47]\d{2}\|6011\|65\d{2})[ -]?\d{4}[ -]?\d{4}[ -]?\d{3,4}\b` | 0.95 | None |
| `SSN` | `\b\d{3}[ -]\d{2}[ -]\d{4}\b` | 0.90 | None |
| `IBAN` | `\b[A-Z]{2}\d{2}[A-Z0-9]{4}[A-Z0-9]{0,26}\b` | 0.90 | None |
| `IP_ADDRESS` | `\b(?:(?:25[0-5]\|2[0-4]\d\|1\d{2}\|[1-9]?\d)\.){3}(?:25[0-5]\|2[0-4]\d\|1\d{2}\|[1-9]?\d)\b` | 0.80 | None (masks IPs in URLs too) |
| `THAI_NATIONAL_ID` | `\b\d{1}[- ]?\d{4}[- ]?\d{5}[- ]?\d{2}[- ]?\d{1}\b` | 0.90 | None |
| `THAI_PHONE` | `(?:\+66\|0)[2-9]\d{1}[- ]?\d{3}[- ]?\d{4}` | 0.90 | Skip if inside URL |

---

## Configuration

### Environment Variables

```bash
PASTEGUARD_ENABLED=true
PASTEGUARD_SECRETS_ENABLED=true
PASTEGUARD_PII_ENABLED=true

# Override default entity list (comma-separated):
# PASTEGUARD_SECRET_ENTITIES=OPENSSH_PRIVATE_KEY,PEM_PRIVATE_KEY,API_KEY_SK,...
# PASTEGUARD_PII_ENTITIES=EMAIL_ADDRESS,PHONE_NUMBER,...
PASTEGUARD_MAX_SCAN_CHARS=200000
```

### Default Enabled Secrets (22 entities)

```
OPENSSH_PRIVATE_KEY, PEM_PRIVATE_KEY, API_KEY_SK, API_KEY_AWS,
API_KEY_GITHUB, API_KEY_GITLAB, JWT_TOKEN, BEARER_TOKEN,
ENV_PASSWORD, ENV_SECRET, CONNECTION_STRING,
API_KEY_GCP, API_KEY_TENCENT, API_KEY_ALIBABA,
API_KEY_SLACK, API_KEY_STRIPE, API_KEY_SENDGRID,
ENV_TOKEN, ENV_CREDENTIAL,
BASIC_AUTH_URL, VAULT_TOKEN, AZURE_CREDENTIAL
```

### Default Enabled PII (8 entities)

```
EMAIL_ADDRESS, PHONE_NUMBER, CREDIT_CARD, SSN,
IBAN, IP_ADDRESS, THAI_NATIONAL_ID, THAI_PHONE
```

---

## Detection Coverage by Leak Type

| Leak scenario | Entity that catches it |
|---------------|----------------------|
| `"token": "Vaka7n3Tv9x8oKBpE"` (Python dict) | `ENV_TOKEN` |
| `CS_CLIENT_SECRET = "r8961h4L..."` | `ENV_SECRET` |
| `CS_CLIENT_ID = "qy91wQ2R..."` | `ENV_CREDENTIAL` |
| `CS_MEMBER_CID = "22RlPmD4..."` | `ENV_CREDENTIAL` |
| `SMTP_PASS = "qO4L32iZ..."` | `ENV_PASSWORD` |
| `DB_PASSWORD=supersecret123` | `ENV_PASSWORD` |
| `https://4.4.4.4/api/v1/user` | `IP_ADDRESS` |
| `postgres://admin:pass@host/db` | `CONNECTION_STRING` |
| `AKID[32-char-secret-id]` | `API_KEY_TENCENT` |
| `LTAI[12-20-char-key]` | `API_KEY_ALIBABA` |
| `xoxb-[token-string]` | `API_KEY_SLACK` |
| `sk_live_[24+char-key]` | `API_KEY_STRIPE` |
| `SG.xxxx.yyyy` | `API_KEY_SENDGRID` |
| `AIzaSyA1234567890abcde...` | `API_KEY_GCP` |
| `https://admin:p@ss@internal.host/api` | `BASIC_AUTH_URL` |
| `hvs.aaaaaaaaaaaaaaaaaaaaaaaa` | `VAULT_TOKEN` |
| `AZURE_CLIENT_SECRET="a1b2c3d4-..."` | `AZURE_CREDENTIAL` |

---

## Not Detected (by design)

| Leak scenario | Reason |
|---------------|--------|
| `SMTP_USER = "n5jZUM..."` | `_USER` too generic, high false positive on `DB_USER`, `APP_USER` |
| `PFUSER = "bro"` | Username without credential pairing, too short |
| `password = "abc"` | Below 8-char minimum |
| Generic high-entropy strings | Shannon entropy not implemented (too many FPs for inline detection) |

---

## Reference Sources

| Tool | Patterns | Key Feature | URL |
|------|----------|-------------|-----|
| Gitleaks | 100+ rules | Keyword + entropy threshold 3.5, allowlist stop-words | https://github.com/gitleaks/gitleaks |
| TruffleHog | 700+ detectors | Verifies secrets against live APIs | https://github.com/trufflesecurity/trufflehog |
| detect-secrets | 30+ plugins | File-type-specific assignment patterns | https://github.com/Yelp/detect-secrets |
| secrets-patterns-db | 1600+ patterns | Largest regex collection | https://github.com/UNX Corp/secrets-patterns-db |
| shhgit | 80 file signatures | Filename + extension matching | https://github.com/eth0izzle/shhgit |

### Key Pattern References

- Gitleaks `generic-api-key`: keywords `["access","api","auth","key","credential","creds","passwd","password","secret","token"]` + value `[\w.=-]{10,150}` + entropy >= 3.5
  - https://github.com/gitleaks/gitleaks/blob/master/cmd/generate/config/rules/generic.go
- TruffleHog Tencent Cloud: `AKID[A-Za-z0-9]{32}`
  - https://github.com/trufflesecurity/trufflehog/issues/4036
- Shannon Entropy for Secret Detection:
  - https://blog.miloslavhomer.cz/secret-detection-shannon-entropy/
