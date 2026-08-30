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

## Agent skills

### Issue tracker

Issues are tracked in GitHub Issues using the `gh` CLI; external PRs are not a triage surface for now. See `docs/agents/issue-tracker.md`.

### Triage labels

Uses the default five-label triage vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout: repo-root `CONTEXT.md` plus `docs/adr/` for ADRs. See `docs/agents/domain.md`.
