# Library Search module

`GET /api/v1/library/search?q=` is the Music Server's Library Search boundary.
It consumes the representations prepared by `internal/searchdata` and owns
matching, ranking, typo fallback, related results, and Best Match selection.
Clients receive ordered groups and never reconstruct relevance from browse pages.

Each request loads a visibility-filtered snapshot of every searchable entity
(`library_search_entities` joined with `library_search_field_sources` and
`library_search_texts`), so ranking, deduplication, and the five-record group
limits apply to the whole eligible library. Playlists are limited to the current
user. Latency measurement and any caching belong to the acceptance spec.

Matching rules, in order of evaluation:

- The query is prepared once with `searchdata.PrepareQuery`. Its folded words are
  matched against each field's folded and punctuation-joined words; the joined
  query form is tried as an alternative. A one-character query returns only
  exact primary-name matches. Empty and punctuation-only queries are `400`.
- A strong direct match covers every query word as a full word; only the final
  word may prefix-match. Internal substrings never match.
- If any strong direct result exists in any type, typo correction is skipped
  entirely. Otherwise each word may use insertion, deletion, substitution, or an
  adjacent transposition: 0 edits for 1–3 characters, 1 for 4–7, 2 for 8 or
  more, at most 2 across the query. Prefix completion and correction are never
  combined within one word. Corrected results are labeled `corrected`, never
  become Best Match, and never expand related records.
- Classes: full primary name; all words within the primary name; words split
  between primary and related fields; related fields only. Within a class the
  order is edits, full-word before prefix-dependent coverage, written-form
  fidelity, version preference (requested allowlisted version, or plain title
  when none is requested), name, Artist/Album text, then identity. Popularity
  and type hierarchy contribute nothing.
- Related results come from the first five strong Tracks whose titles cover a
  query word, expanded once to their Album and their Track and Album Artist
  credits (the Track's own searchable fields; nothing beyond them), deduplicated,
  and ordered by their strongest source Track. A one-character query still
  expands related records from its exact-name Track matches. In Album and Artist groups, exact
  primary-name matches precede related records, which precede weaker direct
  matches; a record appears once at its higher position.
- The Best Match is the top strong direct result across types and is excluded
  from its group. Every group holds at most five remaining records.

`handlers_test.go` is the acceptance seam: the shared
`testutil.SeedLibrarySearchFixture` library served through the router with
every response validated against the OpenAPI contract. The labeled
`testutil.LibrarySearchRankingCases` expectations are reusable by the integrated
quality evaluation. Tests assert identities, order, labels, exclusions, and
errors only. Query text longer than 200 characters is rejected as a request
validation bound.
