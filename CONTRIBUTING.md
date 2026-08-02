# Contributing

Thanks for considering a contribution.

This project is intentionally small: a single self-hosted binary for temporary artifact hosting. Changes should preserve that shape unless there is a strong reason not to.

## Development

```sh
go test ./...
go vet ./...
docker build -t host.sharath.page:test .
```

## Guidelines

- Keep runtime dependencies minimal. Standard library solutions are preferred.
- Preserve streaming behavior for uploads and downloads.
- Avoid features that turn this into a public anonymous file-sharing platform.
- Add or update tests for behavior changes.
- Do not commit generated data, uploaded files, `.env`, or secrets.

## Pull Requests

Before opening a PR, run tests and include a short explanation of the behavior change, risk, and verification performed.
