# API Contracts

## Source of truth

`packages/contracts/openapi.yaml` is the single source of truth for the HTTP API.

All Go and TypeScript types are generated from this file. Do not hand-edit generated output in:

- `server/internal/api/gen/`
- `packages/api-client/src/generated/schema.ts`

## Versioning

- Base path: `/api/v1/`
- Breaking changes require a new major version (`/api/v2/`)
- Non-breaking additions (new optional fields, new endpoints) stay in v1

## Naming

| Item | Convention |
|------|------------|
| JSON fields | camelCase |
| Operation IDs | camelCase (`getHealth`, `patchPreferences`) |
| Error codes | snake_case strings |

## Error format

All error responses use:

```json
{
  "error": "bad_request",
  "code": "bad_request",
  "message": "Human-readable description"
}
```

HTTP status codes follow standard semantics (400, 401, 404, 500).

## Auth (v1 stub)

- Security scheme: Bearer JWT (`bearerAuth`)
- v1 scaffold uses a fixed default user; real auth comes in a later plan
- Protected routes: `GET /api/v1/me`, `GET/PATCH /api/v1/preferences`, playback queue endpoints

## Library (v1)

- Ingestion: Managed Import only (`/api/v1/imports/...`); Legacy Tracks move through the explicit Library Migration (`/api/v1/library-migrations/...`)
- `MUSIC_PATHS` (comma-separated) only locates existing Legacy Tracks; the server never scans it
- Deprecated: `POST /api/v1/library/scan` answers `410 legacy_scan_retired`; `GET /api/v1/library/scan/status` is permanently `idle`. Both stay mounted for API v1 compatibility (ADR 0006, ADR 0015) and new clients must not call them
- Browse: artists, albums, tracks list/detail endpoints under `/api/v1/library/`
- Supported formats: mp3, flac, ogg, m4a, opus, wav

## Playback (v1)

- Queue persisted in SQLite per stub user (`playback_queue` table)
- Stream: `GET /api/v1/tracks/{trackId}/stream` with HTTP Range (`206 Partial Content`)
- No transcoding in v1 — direct file serve

## Preferences / layout contract

`UserPreferences.layout` drives the modular widget shell:

```json
{
  "sidebarPosition": "left",
  "panels": {
    "left": ["now-playing", "queue"],
    "right": ["discover"]
  },
  "collapsed": { "left": false, "right": true }
}
```

Widget IDs must be registered in `packages/ui/src/widgets/registry.tsx`.

## Live updates

Shared Queue invalidations use the OpenAPI-documented server-sent event stream. Party mode remains a future WebSocket feature.

## Codegen

```bash
mise run generate
```

| Target | Tool | Output |
|--------|------|--------|
| Go types | oapi-codegen | `server/internal/api/gen/types.gen.go` |
| TS schema | openapi-typescript | `packages/api-client/src/generated/schema.ts` |

`packages/api-client/src/index.ts` may define convenience wrappers, but exported domain types should alias generated schemas rather than re-declaring fields.

## CI drift check

Generated files are committed. CI runs:

```bash
mise run generate
git diff --exit-code
```

## Swagger UI

- Spec URL: `/api/openapi.yaml`
- UI: `/api/docs`

## Product documentation

Human-readable docs live in `packages/docs/content/*.mdx` and are served at `/docs/`. API reference stays in Swagger; do not duplicate endpoint lists in MDX.

After building docs only, run:

```bash
./scripts/sync-static.sh docs
```
