# Library Search: matching, ranking, and related results

## Problem Statement

The current library dialog combines separate searches, and server queries rely
on substring matching with name-based ordering. Users need the intended record
to win across result types, tolerate bounded spelling mistakes, and see useful
related Albums and Artists without weak results filling the list. Fixed type
priority or unrestricted additive boosts cannot express these requirements.

## Solution

Provide a coherent Library Search result across Tracks, Albums, Artists, Genres,
and Playlists. Require every query word, prefer direct title/name relevance,
fall back to bounded typo correction only when strong direct matches are absent,
and select a cross-type Best Match. Add a limited set of related Albums and
Artists under explicit ranking rules.

## User Stories

1. As a listener, I want a full Track title to find that Track first, so that an Artist type preference cannot hide it.
2. As a listener, I want a full Artist name to outrank matches only in Track credits, so that the intended Artist is easy to reach.
3. As a listener, I want all five library result types considered together, so that I can use one search entry point.
4. As a listener, I want `weeknd blinding lights` to combine title and credit matches, so that I can express a precise target.
5. As a listener, I want query words to match in any order, so that I need not remember the exact title order.
6. As a listener, I want the final word to match a word beginning, so that results appear while I am still typing.
7. As a listener, I want complete-word matches preferred to word beginnings, so that `Blue Moon` precedes `Bluebird` for `blue` within the same class.
8. As a listener, I want a one-character title such as `U` findable, so that short legitimate names remain accessible.
9. As a listener, I want misspellings corrected when no strong direct result exists, so that a small mistake does not end my search.
10. As a listener, I want short words protected from loose correction, so that unrelated short names do not flood the results.
11. As a listener, I want fewer spelling corrections preferred, so that closer fuzzy matches precede more distant ones within the same class.
12. As a listener, I want every query word respected, so that unexpected words are not silently ignored.
13. As a listener, I want empty results when nothing qualifies, so that the app does not pretend unrelated content matches.
14. As a listener, I want faithful spellings and requested versions prioritized within the agreed class, so that exact input remains useful.
15. As a listener, I want plain-title records preferred when I did not request a version, so that version qualifiers do not arbitrarily dominate ties.
16. As a listener, I want a strongly matching Track's Album and Artists discoverable, so that I can explore its immediate context.
17. As a listener, I want a directly exact Album or Artist above an indirectly related one, so that clear textual intent remains primary.
18. As a listener, I want related records deduplicated, so that repeated Track relationships do not waste result slots.
19. As a listener, I want one stable Best Match across types, so that the first highlighted result follows my query rather than a fixed hierarchy.
20. As a listener, I want repeatable results without popularity or history boosts, so that identical searches behave consistently.
21. As a listener, I want search failures distinguishable from no matches, so that I know when to retry.

## Implementation Decisions

- Depend on the normalized data and metadata lifecycle spec. Use its field matrix, normalization rules, and version allowlist without implementing a competing normalizer.
- Put coherent query evaluation behind the Music Server Library Search API boundary. The client must not reconstruct global eligibility by ranking already-truncated, alphabetically sorted pages or downloading the whole library.
- The API must provide stable record identity, result type, original display metadata, a selected Best Match when eligible, and ordered groups. Convey sufficient distinctions between direct and related results to preserve their behavior. The exact operation name and wire shape are engineering choices; document them in the OpenAPI source of truth and regenerate the Go/TypeScript artifacts through the existing generation workflow.
- Preserve existing browse and playback APIs unless a change is necessary for Library Search. Search eligibility follows credited library Artists, including Track credits; an existing Album-Artist-only browse filter must not silently narrow the accepted search scope.
- Normalize query words with the stored-data rules. A strong direct match covers all query words as full words across eligible fields; the final query word alone may use word-prefix matching. Internal substrings do not count as strong matches.
- A one-character query returns only exact primary-name matches. Word and prefix matching start at two query characters. Empty and punctuation-only queries do not trigger search.
- If any strong direct result exists across the five types, do not add typo-only results, even to fill empty groups. Related results do not independently satisfy this gate.
- If no strong direct result exists, permit insertion, deletion, substitution, and adjacent-character transposition, each costing one edit. Apply the following limits to normalized query words, with at most two edits over the whole query:

| Query word length | Maximum edits |
| --- | --- |
| 1–3 characters | 0 |
| 4–7 characters | 1 |
| 8 or more characters | 2 |

- Every query word must still match. Do not introduce stop-word dropping or partial-query fallback. Return no results if none qualify.
- Do not combine correction and prefix completion in the same word. `blind` may prefix-match `blinding`; `blidning` may match it with one transposition; `blid` may not combine both mechanisms. Different words may use different mechanisms, as in `weknd blind`.
- Rank direct results by ordered classes: full primary-name match; all query words within the primary name; query words split between primary and related fields; query words only in related fields. No boost may cross a class boundary.
- Within a class, prefer full-word coverage to prefix-dependent coverage. In fuzzy fallback, compare total edits first, then full-word versus prefix-dependent coverage. Next compare written-form fidelity, then version preference, then alphabetical name, applicable Artist/Album information, and stable record identity.
- Prefer the requested allowlisted version, or the plain-title record when no version is requested, within the preceding constraints. Use this as a bounded ordering criterion; there is no accepted arbitrary additive weight such as 100/85/70.
- Popularity and personalization contributions are zero in the initial version. Do not introduce a type hierarchy as a relevance boost.
- Select at most one Best Match from strong direct results across types. Related-only and typo-only matches are ineligible. Omit Best Match when no eligible result exists.
- For related results, select the first five strong matching Tracks whose titles cover at least one query word. Expand once to their directly associated Albums and Artists; do not expand from typo-only Tracks or follow another relationship hop.
- Deduplicate related records by identity and use their strongest source Track, without summing evidence from many Tracks. Within Album and Artist groups, exact primary-name matches precede related records, which precede weaker direct matches. A record qualifying both ways appears once at its applicable higher position.
- Best Match is excluded from its category's displayed group. Each group has at most five remaining records; direct and related results share that limit. Candidate collection must be sufficient to apply ranking, deduplication, and these limits correctly.
- Validate requests and propagate search failures through existing error conventions rather than returning a successful empty result. Do not expose internal stack traces or scoring implementation details in the user interface.

## Testing Decisions

- Use the Music Server HTTP boundary with real migrated SQLite fixtures as the primary ranking acceptance seam. Assert returned identities, order, group membership, exclusions, and errors, not private score values or SQL structure.
- Reuse existing library HTTP handler tests for normalized credits/Genres and the API's OpenAPI contract checks. Add one coherent Library Search fixture rather than per-stage test-only interfaces.
- Cover all four relevance classes, exact-name precedence across types, original versus folded spelling, full-word versus prefix, one versus two edits, and deterministic ties.
- Test word lengths at every threshold and a multi-word query that exceeds the total edit budget even though each word is individually eligible.
- Test that a strong result in one type disables fuzzy supplementation in every type, and that related results cannot trigger this gate.
- Include all combined-field, prefix, version, punctuation, and negative examples from the shared design. Verify that no query word disappears during matching.
- Exercise the five-source related-result limit, direct-only versus related-only results, deduplication, strongest-source inheritance, nonrecursive expansion, and exact-name precedence over related entries.
- Test Best Match eligibility, removal from groups, shared five-result caps, empty queries, short queries, and failures. These API assertions support, but do not replace, the UI acceptance tests.
- Supply reusable cases to the integrated quality evaluation owned by the presentation and acceptance spec.

## Out of Scope

- Dialog redesign beyond exposing the required result contract; the companion UI spec owns presentation.
- Popularity, listening-history personalization, alias expansion, lyrics, file paths, and Radio search.
- Dropping query words, arbitrary substring matching, unrestricted fuzzy completion, or recursive related-record discovery.
- A mandated external search engine or a new general-purpose search platform.
- Changes to normal library browsing solely to match search behavior.

## Further Notes

This is the second of three broad specs. It depends on search data preparation;
the third spec integrates its results and verifies the complete product targets.
Existing alphabetical browse search is prior behavior, not the ranking contract.
Both follow-up refinements approved after the interview are included: complete
words precede prefixes, and fewer fuzzy edits precede more edits within a class.
