# Security Policy

## Supported Versions

The `develop` branch is the only supported branch at the moment.

## Reporting a Vulnerability

Please report security issues privately through GitHub's private vulnerability reporting, if enabled for the repository. If that is not available, contact the repository owner directly.

Do not open a public issue for vulnerabilities involving token handling, path traversal, file disclosure, denial of service, or uploaded content execution.

## Security Notes

- Uploads require JWT authentication.
- Download links are public to anyone who has the URL.
- Uploaded HTML is intentionally served inline. Use a separate subdomain from sensitive applications.
- This project does not include rate limits, quotas, malware scanning, or abuse automation.
