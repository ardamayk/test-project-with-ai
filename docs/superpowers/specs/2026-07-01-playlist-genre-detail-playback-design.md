# Playlist and Genre Detail Playback Design

## Goal

Make playlist and genre browsing use a detail-first model:

- Collection cards stay clean and only navigate to detail pages.
- Play, shuffle, and queue controls live on each collection detail page.
- Track rows play on double-click and preserve collection context in the queue.
- Remove actions move out of visible row buttons and into right-click context menus.

This keeps showoff cards visually strong without filling overview pages with repeated playback controls.

## Scope

In scope:

- Playlist overview cards link to playlist detail pages.
- Genre overview cards link to genre detail pages.
- Playlist and genre detail pages each show their own collection-specific header, actions, and track list.
- Detail headers include `Play`, `Shuffle`, and `Queue` actions when tracks exist.
- Track double-click starts playback from that track using the active collection's ordered tracks as the queue context.
- Remove/delete controls are not visible inline beside tracks.
- Right-click track menu exposes remove/delete actions appropriate to the page context.
- Tests cover the new playback and remove behavior.

Out of scope:

- Inline play/shuffle buttons on every playlist or genre card.
- Radio/recommendation behavior.
- Auth, permissions, or collaboration behavior.
- Server-side shuffle persistence.

## UX Model

Overview pages are for browsing and choosing a collection. A card click opens detail.

Detail pages are for acting on that collection. The header owns collection-level actions:

- `Play`: plays the first track in the collection and replaces the queue with the collection order.
- `Shuffle`: shuffles only that collection's tracks, replaces the queue with that shuffled order, and starts the first shuffled track.
- `Queue`: appends the collection tracks to the existing queue without starting playback.

Track list behavior:

- Single-click selects/focuses a row only if needed for accessibility or visual focus.
- Double-click plays the clicked track and replaces the queue with the full collection order.
- After the clicked track ends, playback continues through the remaining collection tracks.
- Visible remove buttons are removed from rows.
- Right-click opens a context menu. In playlist detail, it includes `Remove from playlist`. In library-owned views, it keeps destructive delete actions only where the caller explicitly enables library deletion.

## Routes And Pages

Playlist routes:

- `/playlists` shows playlist cards.
- `/playlists/$playlistId` shows one playlist detail page.

Genre routes:

- `/library/genres` shows genre cards.
- `/library/genres/$genre` shows one genre detail page.

Each detail page is collection-specific. It fetches or derives only tracks for that playlist or genre and passes that ordered set to playback actions.

## Components

Reuse and extend existing patterns:

- Keep showoff card styling close to current playlist cards and album grid cards.
- Add a reusable collection detail header when playlist and genre headers share the same title, metadata, artwork, and action layout.
- Reuse `TrackList`, but make row actions context-aware through props.
- Keep album detail behavior unchanged except where shared `TrackList` changes require explicit props.

Proposed `TrackList` behavior props:

- `playMode`: double-click by default for collection detail pages.
- `contextTracks`: ordered tracks used for queue replacement.
- `onRemoveTrack`: optional context-menu action for non-destructive removal.
- `removeLabel`: label for the context-menu item.
- Existing delete behavior remains explicit and only appears when enabled by caller.

## Data Flow

Playlist detail:

1. Load playlist by id.
2. Render playlist metadata and tracks.
3. `Play` calls `playTrack(firstTrack.id, playlist.tracks.map(id))`.
4. `Shuffle` builds a local shuffled track id list, calls `playTrack(shuffled[0], shuffled)`.
5. `Queue` calls `queueTracks(playlist.tracks.map(id))`.
6. `Remove from playlist` calls playlist remove API, then invalidates playlist and playlist list queries.

Genre detail:

1. Load library tracks or albums as needed to derive genre tracks.
2. Filter tracks by normalized genre.
3. Render genre metadata and tracks.
4. `Play`, `Shuffle`, and `Queue` use only the filtered genre track ids.
5. Right-click delete/remove behavior follows existing library delete rules. No visible row remove button.

Shuffle is local UI behavior. It does not change global shuffle mode unless the existing playback provider already does so elsewhere.

## Error And Empty States

- Overview loading/error states remain simple and page-local.
- Detail loading states show collection-specific loading text.
- Missing playlist or genre shows a not-found/error state.
- Empty collection detail disables `Play`, `Shuffle`, and `Queue`, and shows an empty track-list message.
- API mutation failures must leave the current track list visible. Do not optimistically remove tracks for new playlist removal behavior.

## Accessibility

- Cards are links with clear accessible names.
- Header action buttons have text labels and icons.
- Double-click playback must not be the only keyboard path: focused rows support `Enter` to play the row with the same collection queue context.
- Context-menu actions remain reachable through keyboard context menu behavior supplied by the shadcn/Radix context menu.

## Tests

Use TDD before production changes.

Unit/component tests:

- Playlist cards navigate to detail and do not render play/shuffle card actions.
- Playlist detail header renders `Play`, `Shuffle`, and `Queue`.
- `Play` uses playlist track order.
- `Shuffle` uses only playlist or genre tracks and starts from the shuffled first track.
- `Queue` appends only collection tracks.
- Track row double-click calls playback with clicked track id and full collection track ids.
- Track row `Enter` key matches double-click playback.
- Visible remove button is absent.
- Right-click menu exposes `Remove from playlist` when supplied.
- Genre pages derive and display genre-specific tracks.

E2E/Playwright checks:

- `/playlists` card opens its detail page.
- Detail page shows collection actions.
- Track row double-click updates playback/queue UI.
- Right-click track opens remove action.

Verification:

- `pnpm --filter web test`
- `pnpm --filter @repo/ui test` if shared playback or layout behavior changes.
- `pnpm test:e2e` or targeted Playwright smoke when UI is ready.
- `graphify update .` after code changes.

## Implementation Notes

- Do not add play/shuffle buttons to overview cards.
- Keep destructive delete wording distinct from non-destructive playlist removal.
- Use semantic tokens and existing shadcn components.
- Avoid nested cards. Detail header may use the same strong visual treatment as album detail.
- Keep collection order stable for `Play` and double-click. Shuffle only changes order for that one action.
