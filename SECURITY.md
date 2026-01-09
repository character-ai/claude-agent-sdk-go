# Security Policy

## Supported Versions

We release patches for security vulnerabilities in the latest minor version.

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly:

1. **Do not** open a public issue for security vulnerabilities
2. Email security details to the project maintainers
3. Include as much information as possible:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

We will acknowledge receipt within 48 hours and provide a detailed response within 7 days, including next steps and timeline for a fix.

## Security Best Practices

When using this SDK:

- Never commit API keys or secrets to version control
- Use environment variables for sensitive configuration
- Review tool permissions carefully when using `PermissionBypassAll`
- Keep the SDK and its dependencies up to date
- Monitor Dependabot alerts for security updates

## Disclosure Policy

When we receive a security report, we will:

1. Confirm the vulnerability and determine affected versions
2. Develop and test a fix
3. Release a patch and publish a security advisory
4. Credit the reporter (unless they prefer to remain anonymous)
