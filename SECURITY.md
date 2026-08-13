# Security policy

## Supported versions

Only the latest tagged release and the `main` branch receive security fixes.
The project is pre-1.0; compatibility fixes may be backported when practical.

## Reporting a vulnerability

Please use a [private GitHub security advisory](https://github.com/FuqingZh/biofetch/security/advisories/new)
for vulnerabilities. Do not open a public issue for an undisclosed security
problem. Include the affected version or commit, a minimal reproduction, and
the impact. We will acknowledge a report within seven days and coordinate a
fix and disclosure timeline with the reporter.

Never attach passwords, access tokens, cookies, private URLs, browser exports,
or licensed database files to an issue, advisory, pull request, or workflow
artifact. Redact CephFS paths and personally identifying sample data.

Security reports about an upstream database or provider endpoint should be
sent to that provider as well; `biofetch` cannot change upstream authorization
or terms.
