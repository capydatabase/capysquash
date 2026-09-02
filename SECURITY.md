# Security Policy

## Supported Versions

We actively support the following versions with security updates:

| Version | Supported            |
| ------- | -------------------- |
| 0.9.x   | :white_check_mark: |
| < 0.9   | :x:                  |

## Reporting a Vulnerability

**DO NOT** open a public GitHub issue for security vulnerabilities.

### How to Report

Please report security vulnerabilities via:

1. **Email**: <security@capysquash.dev> (preferred)
2. **Private Security Advisory**: [Create Private Report](https://github.com/capysquash/pgsquash-engine/security/advisories/new)

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)
- Your contact information for follow-up

### Response Timeline

- **Initial Response**: Within 48 hours
- **Status Updates**: Every 72 hours until resolved
- **Fix Timeline**: Based on severity
- Critical: 1-7 days
- High: 7-30 days
- Medium: 30-90 days
- Low: Best effort

### Disclosure Policy

We follow responsible disclosure:

1. We will confirm receipt of your report
2. We will investigate and develop a fix
3. We will notify you before public disclosure
4. We will credit you in the security advisory (unless you prefer anonymity)
5. Coordinated disclosure after fix is released

## Security Considerations When Using pgsquash-engine

### Best Practices

- **Always validate** squashed migrations in non-production environments first
- **Use appropriate safety levels**: `paranoid` or `conservative` for production databases
- **Review generated SQL** manually before applying to production
- **Backup production data** before applying any migration changes
- **Use version control** for all migration files (original and squashed)
- **Test rollback procedures** before production deployment

### Safety Levels

Different safety levels provide different risk/optimization tradeoffs:

- **`paranoid`**: Maximum safety, minimal optimization (requires production DB connection)
- **`conservative`**: Safe for production, moderate optimization
- **`standard`**: Balanced approach for staging/testing
- **`aggressive`**: Maximum optimization for development only

### Known Security Considerations

- pgsquash processes SQL but does not execute it by default
- Validation mode requires Docker and PostgreSQL connections
- Plugin transformations should be reviewed when using custom plugins
- Configuration files may contain database connection strings (use environment variables)
- Backup generation features execute `pg_dump` commands (input validation enforced)

### Input Validation

Pgsquash-engine implements input validation for:

- SQL migration files (syntax validation via PostgreSQL parser)
- Configuration parameters (type and range checking)
- Command-line arguments (shell injection prevention)
- Database connection strings (format validation)

### Dependencies

We regularly update dependencies to patch security vulnerabilities. Run:

```bash
go list -m all | go run golang.org/x/vuln/cmd/govulncheck@latest -
```

To check for known vulnerabilities in dependencies.

## Security Updates

Security updates will be announced via:

- GitHub Security Advisories
- CHANGELOG.md with `[SECURITY]` prefix
- GitHub Releases with security badges
- Email notifications for critical vulnerabilities (if you've starred the repo)

### Subscribing to Updates

- **Watch this repository** on GitHub → Custom → Security alerts
- **Star the repository** to receive important announcements
- Follow [@capysquash](https://github.com/CAPYSQUASH) for updates

## Security Audit History

| Date       | Auditor  | Scope             | Findings                     | Status  |
| ---------- | -------- | ----------------- | ---------------------------- | ------- |
| 2025-11-13 | Internal | Pre-release audit | Command injection, dead code | ✅ Fixed |

## Acknowledgments

We thank the following security researchers for responsible disclosure:

- (List will be updated as vulnerabilities are reported and fixed)

## Contact

For security concerns, contact: <security@capysquash.dev>.

For general questions, use [GitHub Discussions](https://github.com/capysquash/pgsquash-engine/discussions)
