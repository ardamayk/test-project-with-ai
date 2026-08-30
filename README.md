# Navidrome Replacement

Self-hosted music server: own your library, stream anywhere on your network, customize the UI with movable panels and widgets.

Not a Navidrome fork — modern OpenAPI backend (Go), React SPA, modular monolith architecture, and room for opt-in plugins/sidecars (discover, party mode, sharing) without legacy Subsonic API lock-in.

## What it does today (v1)

- Scan local music folders (`MUSIC_PATHS`)
- Browse albums and tracks
- Play with queue, seek, and volume (HTTP Range streaming)
- Customizable layout: left/right panels, widgets (now playing, queue)
- Theme: light / dark / system

## Product & design

For **Figma**, **Google Stitch**, or visual design work, start here:

**[docs/DESIGN_BRIEF.md](docs/DESIGN_BRIEF.md)** — product vision, screen inventory, widget shell model, future features, visual direction.

## Stack

| Layer | Tech |
|-------|------|
| Server | Go, chi, SQLite, goose |
| Web | React 19, TanStack Router, Vite, Tailwind 4, shadcn |
| Monorepo | pnpm workspaces, Turborepo |
| API | OpenAPI spec-first (`packages/contracts`) |
| Docs | MDX (`packages/docs`) + Swagger UI (`/api/docs`) |

## Quick start

```bash
mise install
pnpm install
pnpm generate   # OpenAPI codegen
export MUSIC_PATHS=./music   # your music folder(s), comma-separated
mise run dev    # Go :8090 + Vite :3000
```

The Music Server scans `MUSIC_PATHS` once at startup. Open http://localhost:3000/library and use **Scan library** whenever you want an additional rescan.

## Build

```bash
pnpm build
./scripts/sync-static.sh all
cd server && go build -o ../bin/server ./cmd/server
```

## Linux Desktop Client

Run the Music Server and Tauri Desktop Client together in development mode:

```bash
mise run desktop:dev
```

Build and start the release Desktop Client:

```bash
mise run desktop:build
mise run start       # or: pnpm start
```

The launcher automatically selects native Wayland with the NVIDIA explicit-sync workaround when required. Use `EARTHLY_AUDIO_DISPLAY_MODE=x11` to force X11/XWayland or `EARTHLY_AUDIO_DISPLAY_MODE=wayland-shm` for the slower shared-memory Wayland fallback. Docker Compose runs only the Music Server and does not launch the Desktop Client.

## Test

```bash
pnpm test       # unit tests
pnpm test:e2e   # Playwright
```

## Developer docs

- [AGENTS.md](AGENTS.md) — conventions for contributors and AI agents
- [contracts.md](contracts.md) — API contract rules
