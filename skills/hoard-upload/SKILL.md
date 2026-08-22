---
name: hoard-upload
description: Upload files to hoard.sharath.page, a private authenticated file host with expiring links (TTL up to 7 days) and native previews for video/reports. Use when the user wants to share a recording, report, log, or other artifact via a temporary authenticated link, and a HOARD_TOKEN is available.
---

# hoard.sharath.page

Private, invite-only file host. Uploads require a bearer token; shared links are public-by-link but expire automatically. Do not upload secrets.

## Upload

```bash
curl -T <file> \
  -H "Authorization: Bearer $HOARD_TOKEN" \
  "https://hoard.sharath.page/t/$HOARD_TENANT/upload/<filename>?ttl=<duration>"
```

- The service is multitenant: `$HOARD_TENANT` selects the tenant namespace (e.g. `https://hoard.sharath.page/t/myteam/upload/...`). If `$HOARD_TENANT` is unset and the server runs legacy mode, use the unprefixed `/upload/<filename>` path instead.
- `-T` streams the file directly to storage (not a multipart POST).
- `<filename>` in the URL path sets the stored/displayed name — doesn't need to match the local path.
- `ttl` sets link expiry, up to 7 days (e.g. `ttl=3d`, `ttl=12h`).
- `$HOARD_TOKEN` must be set in the environment; there's no anonymous upload. Tokens only work for the tenant they were minted for.

## Auth failures

No/invalid token returns `401` with a JSON body:

```json
{"error":"missing bearer token"}
```

If this happens, ask the user for a valid `HOARD_TOKEN` rather than retrying — there's no fallback anonymous path.

## Notes

- Access is invite-only; the service itself may be offline/online depending on Sharath's infra.
- Native previews exist for video (seekable) and reports (inline HTML) — no special upload flag needed, previews are automatic based on content type.
- Links are public to anyone with the URL — treat uploads as shareable, not private, and never upload credentials/secrets.
