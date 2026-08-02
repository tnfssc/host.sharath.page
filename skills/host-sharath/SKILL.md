---
name: host-sharath
description: Upload files to host.sharath.page, a private authenticated file host with expiring links (TTL up to 7 days) and native previews for video/reports. Use when the user wants to share a recording, report, log, or other artifact via a temporary authenticated link, and a HOST_TOKEN is available.
---

# host.sharath.page

Private, invite-only file host. Uploads require a bearer token; shared links are public-by-link but expire automatically. Do not upload secrets.

## Upload

```bash
curl -T <file> \
  -H "Authorization: Bearer $HOST_TOKEN" \
  "https://host.sharath.page/upload/<filename>?ttl=<duration>"
```

- `-T` streams the file directly to storage (not a multipart POST).
- `<filename>` in the URL path sets the stored/displayed name — doesn't need to match the local path.
- `ttl` sets link expiry, up to 7 days (e.g. `ttl=3d`, `ttl=12h`).
- `$HOST_TOKEN` must be set in the environment; there's no anonymous upload.

## Auth failures

No/invalid token returns `401` with a JSON body:

```json
{"error":"missing bearer token"}
```

If this happens, ask the user for a valid `HOST_TOKEN` rather than retrying — there's no fallback anonymous path.

## Notes

- Access is invite-only; the service itself may be offline/online depending on Sharath's infra.
- Native previews exist for video (seekable) and reports (inline HTML) — no special upload flag needed, previews are automatic based on content type.
- Links are public to anyone with the URL — treat uploads as shareable, not private, and never upload credentials/secrets.
