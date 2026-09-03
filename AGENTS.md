
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


## Modular monolith (future)

## Agent skills

### Issue tracker

Issues are tracked in GitHub Issues using the `gh` CLI; external PRs are not a triage surface for now. See `docs/agents/issue-tracker.md`.

### Triage labels

Uses the default five-label triage vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout: repo-root `CONTEXT.md` plus `docs/adr/` for ADRs. See `docs/agents/domain.md`.
