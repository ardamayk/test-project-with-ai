# Playlist Genre Detail Playback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement playlist and genre detail playback UX from `docs/superpowers/specs/2026-07-01-playlist-genre-detail-playback-design.md`.

**Architecture:** Keep server/API as-is because playlist and track endpoints already exist. Add web route-level detail pages, one reusable collection header, and extend `TrackList` through explicit props for click mode, queue context, delete visibility, and non-destructive remove actions.

**Tech Stack:** React 19, TanStack Router, TanStack Query, Vitest, shadcn/Radix context menu, lucide-react.

---

### Task 1: TrackList Behavior Props

**Files:**
- Modify: `web/src/components/track-list.tsx`
- Test: `web/src/components/track-list.test.tsx`

- [ ] Add failing tests for double-click playback, Enter playback, custom queue context, hidden library delete action, and custom remove context menu action.
- [ ] Run `pnpm --filter web test -- src/components/track-list.test.tsx` and confirm tests fail because props are missing.
- [ ] Add `playMode`, `contextTracks`, `showDelete`, `onRemoveTrack`, and `removeLabel` props.
- [ ] Run the same test command and confirm pass.

### Task 2: Collection Detail Header

**Files:**
- Create: `web/src/components/collection-detail-header.tsx`
- Test: `web/src/components/collection-detail-header.test.tsx`

- [ ] Add failing tests for `Play`, `Shuffle`, and `Queue` actions enabled with tracks and disabled without tracks.
- [ ] Run targeted test and confirm fail.
- [ ] Implement reusable header with title, subtitle, kind label, metadata, icon/art placeholder, and three action buttons.
- [ ] Run targeted test and confirm pass.

### Task 3: Playlist Routes

**Files:**
- Modify: `web/src/routes/playlists/index.tsx`
- Create: `web/src/routes/playlists/$playlistId.tsx`
- Test: `web/src/routes/playlists/playlist-routes.test.tsx`

- [ ] Add failing tests for cards linking to detail and detail actions using playlist track order.
- [ ] Run targeted route test and confirm fail.
- [ ] Make overview cards `Link`s.
- [ ] Implement playlist detail route with query, header actions, `TrackList` double-click mode, and `Remove from playlist` context action.
- [ ] Run targeted route test and confirm pass.

### Task 4: Genre Routes

**Files:**
- Modify: `web/src/routes/library/genres/index.tsx`
- Create: `web/src/routes/library/genres/$genre.tsx`
- Test: `web/src/routes/library/genres/genre-routes.test.tsx`

- [ ] Add failing tests for genre cards derived from tracks and detail actions scoped to matching tracks.
- [ ] Run targeted route test and confirm fail.
- [ ] Implement genre overview from `listTracks({ limit: 200 })`.
- [ ] Implement genre detail route filtering by decoded genre param.
- [ ] Run targeted route test and confirm pass.

### Task 5: Verification

**Files:**
- Generated: `web/src/routeTree.gen.ts` if TanStack Router plugin updates it during tests/build.
- Update graph: `graphify-out/*`

- [ ] Run `pnpm --filter web test`.
- [ ] Run `pnpm test:e2e` if available within environment.
- [ ] Run `graphify update .`.
- [ ] Review `git diff --check` and final diff.
