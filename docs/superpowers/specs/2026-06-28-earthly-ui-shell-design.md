# Earthly Audio UI Shell — Design Spec

**Date:** 2026-06-28  
**Status:** Approved for implementation

## Summary

Earthly Audio branded 3-column music client: left nav + widget dock, center content, right queue + widget dock, bottom player bar. Panel resize via mouse, widget drag-and-drop, multi-preset themes (Earthly, Tokyo Night) with light/dark/system modes.

## Layout

- **Left panel:** Brand, nav (Favorites, Albums, Folders, Radio, Tracks, Playlists, Settings), New Playlist CTA, widget dock, MiniPlayer, Help
- **Center:** MainHeader (search, tabs), page content
- **Right panel:** Fixed Queue (top), widget dock (below)
- **Bottom:** Enhanced PlayerBar
- **Resize:** `react-resizable-panels` — sizes persisted as `[left%, main%, right%]`
- **DnD:** `@dnd-kit` — reorder widgets within dock; move between left/right docks; queue not draggable

## Theme

- `theme.mode`: light | dark | system
- `theme.preset`: earthly | tokyo-night
- Applied via `data-theme-preset` + `.dark` on `document.documentElement`

## API

- `UserPreferences.theme` → `ThemePreferences` object
- `LayoutPreferences.sizes` → `[number, number, number]`

## Routes

| Route | Status |
|-------|--------|
| `/library/albums` | API |
| `/library/tracks` | API |
| `/library/artists` | API |
| `/library/$albumId` | API |
| `/favorites`, `/folders`, `/radio`, `/playlists` | Coming soon |
| `/settings` | Preferences UI |

## shadcn

Components added via CLI in `web/`: resizable, scroll-area, separator, badge, empty, avatar, tabs. UI follows shadcn skill rules (semantic tokens, `flex gap-*`, composition).
