# Figma Shell Bars Design

Date: 2026-07-03

## Goal

Apply the Figma "Navidrome Replacement - Radio Screen" shell treatment to the existing app sidebar and playback bar while preserving current navigation, layout collapse behavior, playback actions, and radio playback integration.

Figma source:
- File: `Z14b5PVvRFZrRr4oO9MNeo`
- Sidebar node: `11:193` (`SideNavBar (Desktop)`)
- Playback node: `11:235` (`Footer / Playback Controls`)

## Scope

In scope:
- Update the desktop sidebar visual styling to match the Figma sidebar.
- Update the bottom playback bar visual styling to match the Figma footer controls.
- Keep the existing three-dot track action menu behavior.
- Fix radio playback progress display so it uses live radio labels instead of track-duration semantics.
- Keep the existing React, Tailwind, shadcn, lucide-react, and design-token approach.

Out of scope:
- Rebuilding the full radio page content.
- Importing short-lived Figma image or SVG asset URLs into production code.
- Changing playback provider APIs unless the bar cannot represent the existing state.
- Changing queue panel, widget dock, preferences, or backend radio behavior.

## Recommended Approach

Use an "exact-ish" implementation based on Figma measurements and colors, adapted to the existing codebase. This keeps the shell visually close to Figma while avoiding short-lived Figma asset URLs and avoiding a large component rewrite.

Existing lucide icons will remain, sized and colored to match the Figma treatment. Hardcoded one-off values may be used only where they represent imported Figma visual constants for this shell surface and no existing semantic token maps cleanly.

## Sidebar Design

The expanded sidebar should use the Figma desktop structure:
- Background: `#132030`.
- Width behavior remains controlled by the existing app shell and layout preferences.
- Header padding: 24px horizontal, 16px top area, with a 32px bottom gap.
- Brand title: `Earthly Audio`, 24px, semibold, `#e6bf9e`.
- Subtitle: `Premium Account`, 12px, medium, letter spacing `0.6px`, `#d3c4b9`.
- Nav list: 16px horizontal margin, 4px vertical item gap.
- Nav item height: 40px with 16px horizontal padding and 16px icon/text gap.
- Inactive item text/icon color: `#d3c4b9`.
- Active item background: `rgba(40, 54, 70, 0.5)`.
- Active item left border: 2px `#e6bf9e`.
- Active item text/icon color: `#e6bf9e`.
- Active item text weight: bold.
- Settings remains pinned at the bottom.

The current disabled `Help (soon)` footer should be removed because it does not exist in the Figma sidebar and competes with the bottom Settings item.

Collapsed sidebar behavior should remain functionally unchanged, but the collapsed icons should receive matching colors and active states.

## Playback Bar Design

The playback bar should follow the Figma footer proportions:
- Height: 72px.
- Background: `rgba(30, 43, 59, 0.95)`.
- Backdrop blur: 12px.
- Top border: subtle `rgba(255, 255, 255, 0.05)`.
- Shadow: upward dark shadow similar to `0 -10px 40px rgba(0, 0, 0, 0.3)`.
- Horizontal padding: 24px.
- Three regions:
  - Now playing: flexible left region, minimum 200px.
  - Controls: centered, max width about 448px.
  - Secondary controls: flexible right region, minimum 150px.

Now playing:
- Artwork should be 48px square with a 2px radius and subtle border.
- Title should be 14px medium with Figma heading color.
- Subtitle should be 11px regular with muted text color.
- The existing three-dot actions button remains next to the title.
- The action menu remains enabled only for track playback unless a separate radio action menu is later designed.

Controls:
- Shuffle, previous, play/pause, next, and repeat stay in the same functional order.
- Play/pause button should be 40px square with 12px radius, Figma accent background `#e6bf9e`, and dark icon color.
- Non-primary control icons should use the muted Figma text color.
- Existing disabled behavior for unsupported controls stays intact.

Progress:
- Track playback keeps normal elapsed time, seek slider, and duration.
- Radio playback shows `LIVE` on the left and `--:--` on the right.
- Radio playback seek remains disabled.
- Radio progress bar should read visually as a live stream indicator, not as elapsed track progress.

Secondary controls:
- Queue toggle remains.
- Quality pill remains disabled for now.
- Track quality should keep bitrate/sample-rate detail.
- Radio quality should use codec and bitrate when available, otherwise `High Quality`.
- Volume remains a compact slider.

## Data Flow

No backend data flow changes are required.

`PlayerBar` should continue deriving display state from `usePlayback()`:
- `currentTrack`
- `currentRadioStation`
- `radioNowPlaying`
- `currentTime`
- `duration`
- `volume`
- playback control callbacks

Radio display priority should remain:
1. `radioNowPlaying.title`
2. `radioNowPlaying.raw`
3. `currentRadioStation.name`
4. fallback empty state

The subtitle should avoid repeating the station name when the title is already the station name. For radio, prefer artist or `Live radio` when no useful artist is present.

## Error Handling

No new error boundary is needed.

Existing playback error handling remains in `PlaybackProvider`. The player bar should continue to render safe fallback labels when no track, station, artwork, or quality metadata is available.

Image fallback behavior stays in `AlbumArt`.

## Testing

Update focused UI tests around:
- Sidebar active radio item visual classes.
- Sidebar no longer renders `Help (soon)`.
- Player bar keeps the three-column centered control layout.
- Empty playback state still disables play.
- Track playback still shows duration-based time and quality details.
- Radio playback shows `LIVE` and `--:--`, disables seek, and does not show misleading track-duration text.
- Radio quality fallback is stable when station metadata is missing.

Run targeted package checks after implementation:
- `pnpm --dir packages/ui test`
- `pnpm --dir packages/ui exec biome check src/layout/SidebarNav.tsx src/layout/PlayerBar.tsx src/layout/PlayerBar.test.tsx src/layout/AppShell.test.tsx`
- `graphify update .`

If visual verification is needed after implementation, use the running app at `/radio` and capture the sidebar and playback bar in the in-app browser.
