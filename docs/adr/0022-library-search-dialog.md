---
status: accepted
---

# One library search dialog instead of per-page search fields

The Albums, Artists and Tracks pages each had a search field in their header, every one scoped to its own page. They are gone; the Top Nav's "Search" entry (also Ctrl/⌘+K, or "/" outside a text field) opens a single dialog that searches Tracks, Albums, Artists, Genres and Playlists at once. Radio keeps its own station search: it filters a saved-station list and the Radio Discover catalog, which are not part of the library.

## Grouped results, tracks first

Results sit under one "From Your Library" heading, grouped by kind in the order Track, Album, Artist, Genre, Playlist, with at most five per group and only non-empty groups shown. Choosing a track plays it; an album, artist, genre or playlist opens its page. Artists have no detail page, so an artist result opens the Artists page with `?q=<name>`, which that route already accepted; the page keeps honouring the URL parameter but no longer renders a field to change it.

## Where the matches come from

Tracks, albums and artists use the server's `q` parameter. Two things the server cannot do are done in the client:

- An album whose title does not match but that holds a matching track is listed under Album as well (searching "Nemo" also surfaces "Decades"); track results carry their album id and title, so no extra request is needed.
- Genres and playlists have no server-side search. Genres are derived from track metadata with the same `collectGenres` helper and query key the Genres page uses; playlists come from the cached playlist list. Both are filtered by name in the client.

## The dialog

It is a Radix Dialog centred in the viewport with a transparent overlay: the page behind stays as it is instead of dimming to black. The input is a combobox over a listbox; the arrow keys move the active row and Enter activates it, Escape closes. Result rows are real buttons so they stay clickable and focusable without custom key handling. The shared `useDebouncedValue` hook (moved out of the Tracks page) holds requests back for 200 ms while typing.
