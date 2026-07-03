# Radio Stations Implementation Plan

## Goal

Build hybrid radio support: users can manage their own stations, import stations from Radio Browser, play radio through the backend, and see best-effort now-playing metadata.

## Decisions Already Made

- Radio stations are user-specific.
- Local DB is source of truth after import.
- Radio Browser is an import/discovery source, not runtime source of truth.
- Radio playback is separate live playback mode, not normal queue playback.
- Backend proxies radio streams and extracts ICY metadata.
- V1 supports station name plus ICY now-playing metadata.
- Imported metadata can be stored in v1; station detail pages and user-facing metadata editing were implemented as a post-v1 slice.
- V1 blocks localhost/private/link-local stream URLs for SSRF safety.
- V1 started with one `/radio` screen; post-v1 station detail pages now cover advanced metadata editing.

## Phase 1: Contract And Data Model

- Add OpenAPI schemas/endpoints for radio station CRUD, Radio Browser search/import, stream proxy, and now-playing.
- Add DB migration for `radio_stations`.
- Include fields for station identity, stream URL, import source, external ID, favicon/homepage, tags, country/language, codec/bitrate, favorite state, position, and last-known now-playing metadata.

## Phase 2: Backend Radio Module

- Add `server/internal/modules/radio`.
- Implement store, handlers, module registration, and tests.
- Add safe outbound stream fetcher with URL validation, redirect limits, and timeouts.
- Add Radio Browser client behind backend endpoint.
- Add ICY metadata parser and now-playing cache.

## Phase 3: API Client

- Regenerate generated OpenAPI types.
- Add typed API client methods for station CRUD, search/import, stream URL, and now-playing.

## Phase 4: Playback Integration

- Extend UI playback model with live radio state.
- Add `playRadioStation` and stop/switch behavior.
- Player bar shows station name and now-playing metadata.
- Track queue stays unchanged.

## Phase 5: Radio UI

- Replace `/radio` placeholder with local station list.
- Add search/filter for local stations.
- Add favorite/all station sections.
- Add manual station form.
- Add Radio Browser discovery/import flow.
- Add station play/favorite/edit/delete actions.

## Phase 6: Verification

- Server unit tests for store, handlers, safe URL validation, Radio Browser mapping, stream proxy, and ICY parser.
- UI tests for station list, import flow, and radio playback state.
- Run server tests, UI tests/build, generated client checks, and `graphify update .`.

## Remaining Future Work

- Drag reorder.
- Visualizer.
- Automatic station sync.
- Scheduled station health checks.
- LAN/private stream support after explicit allowlist design.
- Geolocation-based discovery.
- Advanced ranking.
- HLS ID3 metadata.
- SSE/WebSocket now-playing updates.
