# Library Search Design

Status: Accepted product design, including the two follow-up ranking refinements.
Implementation is specified separately; this document does not claim shipped
behavior or silently supersede existing ADRs.

## Agreed decisions

### Retrieval objective

Prioritize finding an intended Track, Album, or Artist quickly over broad discovery.
Evaluate whether the intended result appears first and within the first five
results. Broader related-result coverage is secondary. Evaluation targets and
thresholds are specified below; individual evaluation queries remain to be built.

### Match precision and typo tolerance

Prefer precise results, including fewer or no results when appropriate, over
filling the list with weak matches. Support typing errors and relax matching
when strong matches are absent.

A strong direct match covers every query word as a complete word across
searchable fields, allowing a word-prefix match for the final query word.
An internal substring does not qualify. If at least one strong direct match
exists, do not supplement results with typo-tolerant matches. Related results
do not independently satisfy this fallback gate.

When no strong direct matches exist, allow insertion, deletion, substitution,
and adjacent-character transposition. Each operation counts as one edit.
Per normalized query word, allow zero edits for 1–3 characters, one edit for
4–7 characters, and two edits for 8 or more characters. Allow no more than
two edits across the entire query. These are agreed initial thresholds to
validate against the evaluation dataset.

### Search scope

Search Tracks, Albums, Artists, Genres, and Playlists. Match Genres and Playlists
by their own names; a matching Track inside a Playlist does not automatically
make that Playlist a result. Radio search remains separate.

| Result type | Primary field | Secondary fields |
| --- | --- | --- |
| Track | Title | Track Artists, Album Artists, Album title, Genres |
| Album | Title | Album Artists |
| Artist | Name | None |
| Genre | Name | None |
| Playlist | Name | None |

Aliases, lyrics, and file paths are outside the initial search scope.

### Exact primary-name precedence

A result whose own name exactly matches the query always ranks ahead of results
matching only related fields. Popularity and listening history cannot override
this precedence. Prefer a match preserving the query's written form over one
requiring accent or punctuation folding.

### Combined-field queries

Allow query words to match across applicable fields, regardless of word order.
For example, `weeknd blinding lights` can match a Track through its Artist credit
and title together. Every query word must be covered, including during fallback.
Correcting a typo is permitted; silently dropping a query word is not. Return
no results when no candidates qualify.

### Text normalization

Support case and punctuation differences, accent-free input, and input without
Turkish-specific characters while preserving original display names. Queries
such as `Beyoncé` / `beyonce`, `Şebnem` / `sebnem`, and
`AC/DC` / `ac dc` / `acdc` should retrieve the corresponding record.
Generate derived search fields when records are stored or metadata is updated;
normalize only the incoming query during search using the same rules. Rebuild
derived fields when normalization rules change. Preserve enough information to
distinguish original-form matches from folded matches. Search normalization must
not change record identity or Album grouping.

Treat case and redundant whitespace differences as equivalent. Retain an
accent-preserving representation alongside an accent-free alternative. The
tolerant representation permits `I`, `İ`, `ı`, and `i` to match; prefer the
representation closer to the query's original letters. Support `don't`, `dont`,
and `don’t`, as well as the punctuation variants above. Do not arbitrarily remove
ordinary word boundaries: `a b` must not match `ab` solely by deleting spaces.
Preserve non-Latin letters and digits. Do not introduce automatic translation or
cross-script transliteration.

### Version qualifiers

Prefer the plain-title record when no version is requested; prefer the requested
version when the query includes a recognized qualifier. Use an explicit allowlist
of version terms and a scoring boost. Do not interpret every parenthetical phrase
as a version or assume a plain title proves an original recording.

The initial allowlist is `live`, `remix`, `acoustic`, `instrumental`, `demo`,
`remaster` / `remastered`, and `radio edit`. In stored titles, recognize these
only in parentheses or a version section separated by ` - `, including dated
forms such as `2011 Remaster`. Query terms may occur anywhere. Version boosts
must preserve exact primary-name precedence. Apply version preference as an
ordering criterion within a match class, not an additive score that crosses classes.

### Related results

Include related records even when their own searchable fields do not match the
query. Within Album and Artist groups, order exact primary-name matches first,
then records related to strongly matching Tracks, then weaker direct matches.
Use only the first five strong matching Tracks whose titles match at least one
query word as sources. Include only their directly associated Albums and Artists,
without recursively expanding relationships. Deduplicate by record identity and
rank each related record by its strongest source Track, without a boost for
multiple matching Tracks. Typo-only Track matches do not generate related
results. The previously agreed exclusion of Playlists
solely because they contain a matching Track remains in effect.

### Short queries

For a one-character query, return only exact primary-name matches. Enable word
and prefix matching from two query characters. Empty and punctuation-only
queries do not trigger search. Per-word typo tolerance still starts at four
characters.

### Tie-breaking signals

Set popularity and personalization contributions to zero in the initial version.
When text relevance and version preference are equal, order alphabetically by
name, then applicable Artist and Album information, then stable record identity.

### Result presentation

Show one cross-type Best Match above the category groups, selected only from
strong direct matches. Omit it when no eligible result exists. Related-only and
typo-only matches are ineligible. Do not repeat the selected record in its group.

Retain a maximum of five displayed results per category, in addition to Best
Match. Direct and related results share the same five-result limit within each
Album or Artist group.

### Prefix and typo interaction

Do not combine prefix completion and typo correction within the same query word.
Different words may use different mechanisms: `weknd blind` may correct the
Artist word and prefix-match the final title word. `blind` can prefix-match
`blinding`, and `blidning` can typo-match it, but `blid` cannot do both to match
`blinding`. All previously agreed word and query edit limits still apply.

### Ranking classes

Rank direct results by the following ordered classes, without summing boosts
across class boundaries:

1. The full primary name matches the query.
2. All query words match within the primary name.
3. Query words match across primary and related fields.
4. Query words match only related fields.

Within a class, first prefer complete-word coverage over prefix-dependent coverage.
During typo fallback, first prefer fewer total edits; for equal edit counts,
prefer complete-word coverage over prefix-dependent coverage. Then prefer
matches preserving the written form, then version preference, then the agreed
deterministic tie-breakers. For example, `blue` ranks `Blue Moon` above
`Bluebird` in the same class; one-edit matches rank above two-edit matches in
the same class. These refinements do not override class boundaries. The strong-match gate
still controls access to typo fallback. Album and Artist groups retain the
previously agreed special placement of related results after exact primary-name
matches and before weaker direct matches. Best Match uses eligible direct results.

### Quality acceptance

Create an initial labeled evaluation set of at least 100 queries. Report query
categories separately. Visibility means appearing in Best Match or within the
first five results in the target's category. Define acceptable target sets when
multiple records are valid answers instead of requiring an arbitrary record.

| Measurement | Initial acceptance threshold |
| --- | --- |
| Correct Best Match for a full-name query with one correct target | 100% |
| Target visibility for error-free word and combined-field queries | At least 95% |
| Target visibility for queries within the allowed typo budget | At least 90% |
| False results on deliberately nonmatching queries | 0 |
| Violations of hard ordering rules or query-word coverage | 0 |

### Latency acceptance

Retain the 200 ms typing debounce. Target server search latency at p95 <= 100 ms
and end-to-end latency from the final keystroke to rendered results at
p95 <= 400 ms. These are acceptance targets, not measured current performance.
Measure against a diverse 50,000-Track catalog with associated Albums and Artists,
using one active search user. Record catalog size and test hardware alongside
results. Use varied queries rather than only repeating cached queries, and report
first-search latency separately.

## Interview completion

All product decisions raised in the interview and both follow-up ranking
refinements have been accepted. Building the labeled query set,
selecting a storage/index implementation, and measuring the agreed targets are
subsequent engineering work; no current performance or quality results are claimed.

## Implementation spec breakdown

1. [Normalized data and metadata lifecycle](2026-09-12-library-search-data-spec.md): derived search representations, supported fields, version recognition, and updates/backfill.
2. [Matching, ranking, and related results](2026-09-12-library-search-ranking-spec.md): coherent search evaluation, the API contract, relevance classes, typo limits, and related-result selection. Depends on spec 1.
3. [Result experience and measured acceptance](2026-09-12-library-search-experience-spec.md): dialog integration, user interactions, shared quality evaluation, and performance evidence. Depends on specs 1 and 2.

The proposed acceptance boundaries reuse real SQLite-backed HTTP integration
tests, existing dialog interaction tests, and a small real-browser journey and
timing suite. Test-boundary confirmation is pending before issue publication.

### Decision coverage

| Interview decisions | Owning spec |
| --- | --- |
| 1–2: retrieval objective and precision | Ranking; experience acceptance |
| 3: five-type scope | Data; ranking |
| 4–5: primary-name precedence and combined queries | Ranking |
| 6, 9, 25: normalization behavior and preparation time | Data |
| 7, 10: version recognition and preference | Data; ranking |
| 8, 11, 16: related results, order, and expansion limits | Ranking |
| 12–15, 20: strong matches, edit limits, word coverage, short queries, prefix interaction | Ranking |
| 17: deterministic ties and zero personalization/popularity | Ranking |
| 18–19: Best Match and group limits | Ranking contract; experience integration |
| 21: ordered relevance classes and bounded boosts | Ranking |
| 22: quality acceptance and labeled dataset | Experience acceptance |
| 23, 26: latency budgets and benchmark scale | Experience acceptance |
| 24: searchable field matrix | Data; ranking |
| Follow-up: full words before prefixes; fewer edits before more | Ranking |

## Existing decision to reconcile

[ADR-0022](../../adr/0022-library-search-dialog.md) specifies grouped results in
Track, Album, Artist, Genre, Playlist order. The agreed Best Match area changes
that presentation rule by preceding the existing groups. Reconcile the ADR after
the interview is confirmed complete; this draft does not silently supersede it.

The example weights in the discussion (100, 85, 70, and others) are illustrative,
not accepted ranking parameters.
