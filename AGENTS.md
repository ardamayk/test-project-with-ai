# Agent Guide — Navidrome Replacement

Read this file before making changes. It describes stack, layout, packages, and automation rules.

## Architecture

```text
packages/contracts/openapi.yaml   ← source of truth
        ↓ generate
packages/api-client               ← TypeScript client
server/                           ← Go API + static embed
web/                              ← React SPA (dev proxy → Go)
packages/ui                       ← shared layout/widget shell
packages/docs                     ← MDX product docs → /docs
```

Production: Go binary embeds `web/dist` and `packages/docs/dist` (via `scripts/sync-static.sh`).

## Stack


| Area      | Technologies                                                                         |
| --------- | ------------------------------------------------------------------------------------ |
| Server    | Go 1.23+, chi, SQLite (modernc), goose, slog, go:embed                               |
| Web       | React 19, TanStack Router, TanStack Query, Vite 8, Tailwind 4, shadcn, Biome, Vitest |
| Monorepo  | pnpm workspaces, Turborepo, mise                                                     |
| Contracts | OpenAPI 3.1 spec-first                                                               |
| E2E       | Playwright (`@playwright/test`, `playwright-cli` skill)                              |
| Docs      | MDX (`@mdx-js/rollup`) + Swagger UI at `/api/docs`                                   |




## Web dependencies (`web/package.json`)



### Runtime


| Package                                    | Purpose                                       |
| ------------------------------------------ | --------------------------------------------- |
| `@repo/api-client`                         | Generated OpenAPI TypeScript client           |
| `@repo/ui`                                 | AppShell, widget registry, layout preferences |
| `@sentry/react`                            | Client error monitoring                       |
| `@t3-oss/env-core` + `zod`                 | Typed env vars                                |
| `@tanstack/react-router`                   | File-based routing                            |
| `@tanstack/react-query`                    | Server state / API cache                      |
| `@tanstack/react-form`                     | Forms                                         |
| `radix-ui` + shadcn components             | UI primitives                                 |
| `tailwindcss` + `class-variance-authority` | Styling                                       |
| `lucide-react`                             | Icons                                         |




### Dev


| Package                                              | Purpose       |
| ---------------------------------------------------- | ------------- |
| `@biomejs/biome`                                     | Lint + format |
| `@playwright/test`                                   | E2E tests     |
| `vitest` + `@testing-library/react`                  | Unit tests    |
| `@vitejs/plugin-react` + React Compiler babel plugin | Build         |
| `@tanstack/router-plugin`                            | Route codegen |




## Directory rules


| Change type         | Location                                            |
| ------------------- | --------------------------------------------------- |
| API contract        | `packages/contracts/openapi.yaml` → `pnpm generate` |
| Go handlers         | `server/internal/api/`                              |
| Domain logic        | `server/internal/modules/<name>/`                   |
| DB migrations       | `server/migrations/`                                |
| Web routes          | `web/src/routes/`                                   |
| Shared UI / layout  | `packages/ui/src/`                                  |
| Widget registration | `packages/ui/src/widgets/registry.tsx`              |
| Product docs (MDX)  | `packages/docs/content/`                            |




## Modular monolith (future)

`server/internal/modules/` holds domain modules. Each implements:

```go
type Module interface {
    Name() string
    RegisterRoutes(r chi.Router)
}
```

v1 modules: `preferences`, `library`, `playback` implemented; future modules follow the same pattern.

## Commands

```bash
mise run dev          # Go API :8090 + Vite dev :3000
pnpm generate         # OpenAPI → Go types + TS schema
pnpm build            # turbo: build all packages
pnpm test             # unit tests
pnpm test:e2e         # Playwright smoke
./scripts/sync-static.sh   # copy dist → Go embed paths (before go build)
```



## Agent automation rules


| When                            | Action                                                    |
| ------------------------------- | --------------------------------------------------------- |
| **Before** codebase exploration | `graphify query "<question>"` — do not skip for Read/Grep |
| **After** code file changes     | `graphify update .`                                       |
| New feature / behavior change   | `brainstorming` skill first                               |
| Multi-step implementation       | `writing-plans` → `executing-plans`                       |
| Bug or test failure             | `systematic-debugging`                                    |
| Feature or bugfix code          | `test-driven-development`                                 |
| E2E verification                | `playwright-cli` or `pnpm test:e2e`                       |
| User communication style        | **caveman mode active** — terse, no filler                |
| Before claiming done            | `verification-before-completion` — run tests, show output |




## Graphify

Knowledge graph output: `graphify-out/`. After code changes:

```bash
graphify update .
# or full rebuild (requires GEMINI_API_KEY or GOOGLE_API_KEY when markdown docs are in corpus):
graphify . --wiki
```



## API changes checklist

1. Edit `packages/contracts/openapi.yaml`
2. Run `pnpm generate`
3. Implement Go handler in appropriate module
4. Use `@repo/api-client` in web
5. Add/update tests
6. `graphify update .`



## Testing pyramid

- Go handler logic → `*_test.go` in `server/`
- React / UI → Vitest in `web/` and `packages/ui/`
- API client → Vitest in `packages/api-client/`
- Critical flows → Playwright in `web/e2e/`



## Skills in repo

- `.claude/skills/playwright-cli/SKILL.md` — browser automation for E2E debugging
- Always use caveman skill and adjust caveman skill level according to task. lite, full or ultra modes allowed.

## UI: Earthly Audio shell

| Area | Location |
|------|----------|
| App shell (3-column resize + DnD) | `packages/ui/src/layout/` |
| Widget registry | `packages/ui/src/widgets/registry.tsx` |
| Theme presets | `web/src/themes/earthly.css`, `tokyo-night.css` |
| Theme apply | `packages/ui/src/theme/ThemeProvider.tsx` + `web/src/components/theme-sync.tsx` |
| shadcn components | `web/components.json` → `web/src/components/ui/` |
| Web routes | `web/src/routes/` |

### shadcn/ui

- Run CLI from `web/`: `pnpm dlx shadcn@latest add <component>`
- Use semantic tokens (`bg-primary`, `text-muted-foreground`); no raw color classes
- Spacing: `flex flex-col gap-*` (not `space-y-*`)
- See shadcn skill for composition rules (Card, Empty, Badge, Field, etc.)

### Theme system

- `UserPreferences.theme`: `{ mode: light|dark|system, preset: earthly|tokyo-night }`
- Applied via `data-theme-preset` on `<html>` + `.dark` class for mode
- CSS variables in `web/src/styles.css` + preset files under `web/src/themes/`

### Layout customization

- Panel resize: shadcn `Resizable` / `react-resizable-panels` in `AppShell`; sizes in `layout.sizes` `[left, main, right]` %
- **Important:** `react-resizable-panels` v4 — numeric `minSize`/`maxSize`/`defaultSize` are **pixels**; use string `"22"` or `"22%"` for percentages
- Widget DnD: `@dnd-kit` in `WidgetDock`; queue is fixed in right panel (not a widget)
- Preferences sync: `PATCH /api/v1/preferences`

## Intentionally out of scope (v1 scaffold)

Auth implementation, party WebSocket, sidecars, Lua scripting, native client. Playlists/favorites/folders/radio UI placeholders only.

See `contracts.md` for API contract rules.