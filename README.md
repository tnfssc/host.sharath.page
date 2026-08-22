# hoard.sharath.page

Small self-hosted temporary file host for agent artifacts, screen recordings, logs, reports, and other short-lived files.

It is designed for one simple workflow:

```sh
url=$(hoard-upload recording.mp4)
printf '%s\n' "$url"
```

Uploads require a JWT. Downloads are public to anyone with the unguessable link.

The service is multitenant: each tenant gets an isolated namespace under `/t/<tenant>/`, its own JWT signing secret, its own admin token, and optional per-tenant limits (TTL and upload size). Tenants are managed with the root admin token or the `hoard tenant` CLI. A legacy single-tenant mode (global `JWT_SECRET`, unprefixed routes) is still available for existing deployments.

## Features

- Streams large uploads to disk without buffering them in memory
- Serves files inline so videos can play/seek and HTML reports can render
- Supports HTTP range requests for video seeking
- Auto-expires files after a configurable TTL
- Uses short public links: `/t/<tenant>/f/<5-char-id>/<clean-filename>`
- Isolates tenants: separate secrets, storage directories, and limits
- Keeps dependencies to zero runtime Go modules: standard library only
- Runs as a single distroless Docker image
- Includes a tiny `hoard-upload` CLI wrapper for remote machines and agents

## Quick Start

```sh
cp .env.example .env
$EDITOR .env
docker compose up -d --build
```

Generate the root admin token for `.env`:

```sh
openssl rand -hex 32
```

Create a tenant and save the secrets it prints (they are only shown once):

```sh
docker compose exec hoard /hoard tenant create --max-ttl 7d myteam
```

Mint an upload token for that tenant:

```sh
docker compose exec hoard /hoard mint --tenant myteam --name laptop --days 365
```

Upload from an agent or remote machine:

```sh
export HOARD_URL=https://hoard.sharath.page
export HOARD_TENANT=myteam
export HOARD_TOKEN='<jwt>'
./cli/hoard-upload recording.mp4 3d
```

Raw `curl` works too:

```sh
curl -fsS -T recording.mp4 \
  -H "Authorization: Bearer $HOARD_TOKEN" \
  "$HOARD_URL/t/myteam/upload/recording.mp4?format=text&ttl=3d"
```

## Configuration

Configuration is environment-based.

| Variable | Default | Description |
| --- | --- | --- |
| `ADDR` | `:8080` | HTTP listen address |
| `DATA_DIR` | `./data` | Directory used for uploaded files, metadata, and the tenant registry |
| `BASE_URL` | request-derived | Public base URL used in returned links |
| `ADMIN_TOKEN` | required | Root admin secret for tenant management and token minting |
| `JWT_SECRET` | unset | Optional HS256 secret enabling the legacy single-tenant routes |
| `DEFAULT_TTL` | `72h` | Default file lifetime |
| `MAX_TTL` | `7d` | Maximum accepted upload TTL |
| `MAX_UPLOAD_BYTES` | `5368709120` | Upload limit in bytes, default 5 GiB |
| `SWEEP_INTERVAL` | `1m` | Expiry janitor interval |

Durations accept Go duration strings like `30m`, `12h`, `72h`, plus day strings like `3d`.

## Tenants

Tenants are isolated namespaces stored under `DATA_DIR/t/<name>/`, registered in `DATA_DIR/tenants.json`. Each tenant has its own JWT secret (tokens are signed per tenant), its own admin token (can mint tokens for itself only), and optional limits that override the global defaults.

Manage tenants with the CLI (runs against `DATA_DIR` directly):

```sh
hoard tenant create [--default-ttl 72h] [--max-ttl 7d] [--max-upload-bytes N] <name>
hoard tenant list
hoard tenant delete <name>   # also removes all of the tenant's files
```

Or with the HTTP API, using the root `ADMIN_TOKEN`:

```http
POST /api/tenants
X-Admin-Token: <root-admin-token>
Content-Type: application/json

{"name":"myteam","default_ttl":"72h","max_ttl":"7d","max_upload_bytes":1073741824}
```

`GET /api/tenants` lists tenants (secrets are never included), and `DELETE /api/tenants/<name>` removes a tenant and all its files. The secrets from the create response are shown only once; losing them means re-creating the tenant.

## API

All tenant-scoped endpoints live under `/t/<tenant>/`. The legacy unprefixed endpoints (`/upload`, `/f/...`, `/api/tokens`) behave identically for the legacy tenant and are only available when `JWT_SECRET` is set.

### Upload Raw Body

```http
PUT /t/<tenant>/upload/<filename>?ttl=3d&format=text
Authorization: Bearer <jwt>
```

The request body is streamed directly to disk. `format=text` returns only the public URL, which is convenient for shell scripts.

### Upload Multipart

```http
POST /t/<tenant>/upload?ttl=3d
Authorization: Bearer <jwt>
Content-Type: multipart/form-data
```

Use form field `file`.

### Serve File

```http
GET /t/<tenant>/f/<id>/<filename>
```

Files are served with `Content-Disposition: inline`, a detected or extension-based `Content-Type`, and range support. A file is only visible under the tenant that uploaded it.

### Mint Token

```http
POST /t/<tenant>/api/tokens
X-Admin-Token: <root-or-tenant-admin-token>
Content-Type: application/json

{"name":"agent-1","days":365}
```

## Deployment Notes

The compose file binds the service to `127.0.0.1:8787` by default. Put your preferred reverse proxy or tunnel in front of it.

Cloudflare's orange-cloud proxy and most Cloudflare Tunnel setups enforce request body limits that are too small for multi-GB uploads on non-enterprise plans. For large uploads, use a grey-cloud DNS record, direct host/port, or another upload path that bypasses Cloudflare's request body cap. Downloads through Cloudflare are fine.

## Security Model

This is a personal artifact host, not a public anonymous upload service.

Uploads require a valid JWT signed with the tenant's own secret, so a compromised tenant token cannot be used against other tenants. Downloads are public-by-link. Anyone with a file URL can view that file until it expires.

The root `ADMIN_TOKEN` controls tenant creation/deletion and can mint tokens for any tenant; tenant admin tokens can only mint tokens for their own tenant. The tenant registry (`DATA_DIR/tenants.json`) contains per-tenant secrets and is stored with `0600` permissions — protect the data dir accordingly.

Uploaded HTML is intentionally served inline. If you host this on a domain that also carries sensitive cookies or login state, use a separate subdomain for file content.

For internet-facing production use, add rate limiting, per-tenant quotas, abuse monitoring, audit logs, longer IDs, and deletion/revocation endpoints.

## Development

```sh
go test ./...
go vet ./...
docker build -t hoard.sharath.page:test .
```

Run locally:

```sh
export ADMIN_TOKEN=dev-admin-token-123456789
go run .
```

Create a tenant and mint a local token:

```sh
go run . tenant create dev
go run . mint --tenant dev --name dev --days 1
```

## License

MIT
