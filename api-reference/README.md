# Official API specs

Snapshots of the official bunny.net OpenAPI specs, downloaded on 2026-11-01.
They are the contract the SDK is tested against; refresh them with the `curl`
commands below and review the diff.

| File | Source |
|---|---|
| `storage.json` | https://bunny.net/docs/api-reference/storage/openapi.json |
| `stream.json` | https://video.bunnycdn.com/openapi/bunnynet-video-api.public.json |
| `core.json` | https://core-api-public-docs.b-cdn.net/docs/v3/public.json |

```sh
curl -fL -o storage.json https://bunny.net/docs/api-reference/storage/openapi.json
curl -fL -o stream.json  https://video.bunnycdn.com/openapi/bunnynet-video-api.public.json
curl -fL -o core.json    https://core-api-public-docs.b-cdn.net/docs/v3/public.json
```
