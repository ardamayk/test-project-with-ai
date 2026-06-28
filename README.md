# Navidrome Replacement

Self-hosted music server monorepo: Go API + React SPA + modular widget layout.

## Stack

| Layer | Tech |
|-------|------|
| Server | Go 1.23, chi, SQLite, goose |
| Web | React 19, TanStack Router, Vite, Tailwind 4, shadcn |
| Monorepo | pnpm workspaces, Turborepo |
| API | OpenAPI spec-first (`packages/contracts`) |
| Docs | MDX (`packages/docs`) + Swagger UI (`/api/docs`) |

## Quick start

```bash
mise install
pnpm install
pnpm generate   # OpenAPI codegen
mise run dev    # Go :8090 + Vite :3000
```

## Build

```bash
pnpm build
./scripts/sync-static.sh
cd server && go build -o ../bin/server ./cmd/server
```

## Test

```bash
pnpm test       # unit tests
pnpm test:e2e   # Playwright (starts API + web dev)
```

## Layout

See [AGENTS.md](./AGENTS.md) for agent conventions and [contracts.md](./contracts.md) for API rules.
