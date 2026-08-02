# host.sharath.page

Small self-hosted temporary file host for agent artifacts, screen recordings, logs, reports, and other short-lived files.

It is designed for one simple workflow:

```sh
url=$(host-upload recording.mp4)
printf '%s\n' "$url"
```

Uploads require a JWT. Downloads are public to anyone with the unguessable link.

## Features

- Streams large uploads to disk without buffering them in memory
- Serves files inline so videos can play/seek and HTML reports can render
- Supports HTTP range requests for video seeking
- Auto-expires files after a configurable TTL
- Uses short public links: `/f/<5-char-id>/<clean-filename>`
- Keeps dependencies to zero runtime Go modules: standard library only
- Runs as a single distroless Docker image
- Includes a tiny `host-upload` CLI wrapper for remote machines and agents

## Quick Start

```sh
cp .env.example .env
$EDITOR .env
docker compose up -d --build
```

Generate secrets for `.env`:

```sh
openssl rand -hex 32
```

Mint an upload token:

```sh
docker compose exec host /host mint --name laptop --days 365
```

Upload from an agent or remote machine:

```sh
export HOST_URL=https://host.sharath.page
export HOST_TOKEN='<jwt>'
./cli/host-upload recording.mp4 3d
```

Raw `curl` works too:

```sh
curl -fsS -T recording.mp4 \
  -H "Authorization: Bearer $HOST_TOKEN" \
  "$HOST_URL/upload/recording.mp4?format=text&ttl=3d"
```

## Configuration

Configuration is environment-based.

| Variable | Default | Description |
| --- | --- | --- |
| `ADDR` | `:8080` | HTTP listen address |
| `DATA_DIR` | `./data` | Directory used for uploaded files and metadata |
| `BASE_URL` | request-derived | Public base URL used in returned links |
| `ADMIN_TOKEN` | required | Shared secret for `POST /api/tokens` |
| `JWT_SECRET` | required | HS256 signing secret for upload tokens |
| `DEFAULT_TTL` | `72h` | Default file lifetime |
| `MAX_TTL` | `7d` | Maximum accepted upload TTL |
| `MAX_UPLOAD_BYTES` | `5368709120` | Upload limit in bytes, default 5 GiB |
| `SWEEP_INTERVAL` | `1m` | Expiry janitor interval |

Durations accept Go duration strings like `30m`, `12h`, `72h`, plus day strings like `3d`.

## API

### Upload Raw Body

```http
PUT /upload/<filename>?ttl=3d&format=text
Authorization: Bearer <jwt>
```

The request body is streamed directly to disk. `format=text` returns only the public URL, which is convenient for shell scripts.

### Upload Multipart

```http
POST /upload?ttl=3d
Authorization: Bearer <jwt>
Content-Type: multipart/form-data
```

Use form field `file`.

### Serve File

```http
GET /f/<id>/<filename>
```

Files are served with `Content-Disposition: inline`, a detected or extension-based `Content-Type`, and range support.

### Mint Token

```http
POST /api/tokens
X-Admin-Token: <admin-token>
Content-Type: application/json

{"name":"agent-1","days":365}
```

## Deployment Notes

The compose file binds the service to `127.0.0.1:8787` by default. Put your preferred reverse proxy or tunnel in front of it.

Cloudflare's orange-cloud proxy and most Cloudflare Tunnel setups enforce request body limits that are too small for multi-GB uploads on non-enterprise plans. For large uploads, use a grey-cloud DNS record, direct host/port, or another upload path that bypasses Cloudflare's request body cap. Downloads through Cloudflare are fine.

## Security Model

This is a personal artifact host, not a public anonymous upload service.

Uploads require a valid JWT. Downloads are public-by-link. Anyone with a file URL can view that file until it expires.

Uploaded HTML is intentionally served inline. If you host this on a domain that also carries sensitive cookies or login state, use a separate subdomain for file content.

For internet-facing production use, add rate limiting, per-user quotas, abuse monitoring, audit logs, longer IDs, and deletion/revocation endpoints.

## Development

```sh
go test ./...
go vet ./...
docker build -t host.sharath.page:test .
```

Run locally:

```sh
export ADMIN_TOKEN=dev-admin-token-123456789
export JWT_SECRET=dev-jwt-secret-123456789
go run .
```

Mint a local token:

```sh
JWT_SECRET=dev-jwt-secret-123456789 go run . mint --name dev --days 1
```

## License

MIT
