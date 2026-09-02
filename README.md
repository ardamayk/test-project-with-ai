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
mise run generate   # OpenAPI codegen
export MUSIC_PATHS=./music   # your music folder(s), comma-separated
mise run dev    # Go :8090 + Vite :3000
```

The Music Server scans `MUSIC_PATHS` once at startup. Open http://localhost:3000/library and use **Scan library** whenever you want an additional rescan.

## Build

```bash
mise run build
```

Mise is the canonical command interface. Use `mise run web:build`, `mise run server:build`, or `mise run desktop:build` for one artifact.

## Linux Desktop Client

Run the Music Server and Tauri Desktop Client together in development mode:

```bash
mise run desktop:dev
```

Build and start the release Desktop Client:

```bash
mise run build
mise run start       # or: pnpm start
```

The launcher automatically selects native Wayland with the NVIDIA explicit-sync workaround when required. Use `EARTHLY_AUDIO_DISPLAY_MODE=x11` to force X11/XWayland or `EARTHLY_AUDIO_DISPLAY_MODE=wayland-shm` for the slower shared-memory Wayland fallback. Docker Compose runs only the Music Server and does not launch the Desktop Client.

## Test

```bash
mise run ci:fast          # static checks and unit tests
mise run ci:integration   # Playwright, HLS, and pinned-mpv tests
mise run ci:full          # fast + integration + production builds
```

Targeted commands include `mise run web:test`, `mise run ui:test`, `mise run api-client:test`, `mise run server:test`, and `mise run desktop:test`. Root pnpm commands remain compatibility proxies; new development and CI automation should call Mise.

## Git hooks

`pnpm install` configures repository-managed Husky hooks for local development. Pre-commit formats and checks staged JavaScript, TypeScript, JSON, Go, and Rust source, preserving partial staging, and runs OpenAPI drift verification only when generation inputs are staged. Pre-push runs the complete `mise run ci:fast` policy.

Hooks provide fast local feedback; CI remains authoritative. Use Git's `--no-verify` option when a hook must be bypassed. Hook installation is disabled in CI and production dependency installs.

If `graphify-out/` exists and `graphify` is available on `PATH`, post-commit and branch-switch post-checkout hooks update the graph in the background. Missing Graphify is an intentional no-op; no machine-specific executable path is required.

## Developer docs

- [AGENTS.md](AGENTS.md) — conventions for contributors and AI agents
- [contracts.md](contracts.md) — API contract rules
