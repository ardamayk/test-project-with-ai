package librarysearch

import (
	"slices"
	"sort"
	"strings"

	apigen "github.com/ardam/navidrome-replacement/server/internal/api/gen"
	"github.com/ardam/navidrome-replacement/server/internal/searchdata"
)

// Relevance classes. No boost crosses a class boundary.
const (
	CLASS_FULL_NAME       = 1 // the whole primary name matches the query
	CLASS_PRIMARY_ONLY    = 2 // every query word matches within the primary name
	CLASS_PRIMARY_RELATED = 3 // query words split between primary and related fields
	CLASS_RELATED_ONLY    = 4 // query words match only related fields
)

const (
	MAX_TOTAL_EDITS     = 2
	MAX_RELATED_SOURCES = 5
	GROUP_LIMIT         = 5
)

type query struct {
	variants [][]string // folded words, plus the punctuation-joined alternative when it differs
	faithful []string
	versions []string
	single   bool // a one-character query returns only exact primary-name matches
}

func newQuery(text searchdata.Text) query {
	folded := strings.Fields(text.Folded)
	q := query{variants: [][]string{folded}, faithful: splitFaithful(text.Faithful), versions: text.Versions}
	if compact := strings.Fields(text.Compact); text.Compact != text.Folded {
		q.variants = append(q.variants, compact)
	}
	q.single = len([]rune(text.Folded)) == 1
	return q
}

func (q query) isEmpty() bool { return len(q.variants[0]) == 0 }

// maxEdits bounds typo correction per normalized query word.
func maxEdits(word string) int {
	switch length := len([]rune(word)); {
	case length <= 3:
		return 0
	case length <= 7:
		return 1
	default:
		return 2
	}
}

type matchKind int

const (
	matchNone matchKind = iota
	matchFull
	matchPrefix
	matchFuzzy
)

type wordMatch struct {
	kind  matchKind
	edits int
}

func (m wordMatch) better(other wordMatch) bool {
	if m.kind == matchNone || other.kind == matchNone {
		return m.kind != matchNone
	}
	if m.edits != other.edits {
		return m.edits < other.edits
	}
	return m.kind < other.kind
}

// matchWord finds how one query word matches one prepared text. Prefix
// completion and typo correction are never combined within a word.
func matchWord(word string, text *preparedText, allowPrefix bool, limit int) wordMatch {
	best := wordMatch{}
	for _, words := range [][]string{text.foldedWords, text.compactWords} {
		for _, candidate := range words {
			if candidate == word {
				return wordMatch{kind: matchFull}
			}
			if allowPrefix && strings.HasPrefix(candidate, word) {
				best = wordMatch{kind: matchPrefix}
			}
			if limit > 0 && best.kind != matchPrefix {
				if edits := editDistance(word, candidate, limit); edits <= limit && (best.kind == matchNone || edits < best.edits) {
					best = wordMatch{kind: matchFuzzy, edits: edits}
				}
			}
		}
	}
	return best
}

// editDistance is the optimal string alignment distance (insertion, deletion,
// substitution, adjacent transposition), returning limit+1 once it is exceeded.
func editDistance(a, b string, limit int) int {
	left, right := []rune(a), []rune(b)
	if difference := len(left) - len(right); difference > limit || -difference > limit {
		return limit + 1
	}
	rows := make([][]int, len(left)+1)
	for i := range rows {
		rows[i] = make([]int, len(right)+1)
		rows[i][0] = i
	}
	for j := range rows[0] {
		rows[0][j] = j
	}
	for i := 1; i <= len(left); i++ {
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			value := min(rows[i-1][j]+1, rows[i][j-1]+1, rows[i-1][j-1]+cost)
			if i > 1 && j > 1 && left[i-1] == right[j-2] && left[i-2] == right[j-1] {
				value = min(value, rows[i-2][j-2]+1)
			}
			rows[i][j] = value
		}
	}
	return rows[len(left)][len(right)]
}

type candidate struct {
	entity          *entity
	class           int
	edits           int
	prefixDependent bool
	faithful        int
	versionRank     int
	name            string
	secondary       string
}

// less is the complete within-class ordering; class remains the outer key.
func (c candidate) less(other candidate) bool {
	switch {
	case c.class != other.class:
		return c.class < other.class
	case c.edits != other.edits:
		return c.edits < other.edits
	case c.prefixDependent != other.prefixDependent:
		return !c.prefixDependent
	case c.faithful != other.faithful:
		return c.faithful > other.faithful
	case c.versionRank != other.versionRank:
		return c.versionRank < other.versionRank
	case c.name != other.name:
		return c.name < other.name
	case c.secondary != other.secondary:
		return c.secondary < other.secondary
	case c.entity.id != other.entity.id:
		return c.entity.id < other.entity.id
	default:
		return c.entity.kind < other.entity.kind
	}
}

func evaluate(item *entity, q query, fuzzy bool) (candidate, bool) {
	var best candidate
	found := false
	for _, words := range q.variants {
		if result, ok := evaluateVariant(item, words, q, fuzzy); ok && (!found || result.less(best)) {
			best, found = result, true
		}
	}
	return best, found
}

func evaluateVariant(item *entity, words []string, q query, fuzzy bool) (candidate, bool) {
	limits := make([]int, len(words))
	if fuzzy {
		for i, word := range words {
			limits[i] = maxEdits(word)
		}
	}
	result := candidate{entity: item, name: item.primary.Folded, secondary: secondaryText(item)}
	if q.single {
		if !slices.Equal(words, item.primary.foldedWords) && !slices.Equal(words, item.primary.compactWords) {
			return candidate{}, false
		}
		result.class = CLASS_FULL_NAME
		return finish(result, q, item, false), true
	}
	if edits, ok := alignedEdits(words, item.primary, limits); ok {
		result.class, result.edits = CLASS_FULL_NAME, edits
		return finish(result, q, item, false), true
	}
	primary := make([]wordMatch, len(words))
	related := make([]wordMatch, len(words))
	last := len(words) - 1
	for i, word := range words {
		primary[i] = matchWord(word, item.primary, i == last, limits[i])
		for _, field := range item.related {
			if m := matchWord(word, field.text, i == last, limits[i]); m.better(related[i]) {
				related[i] = m
			}
		}
		if primary[i].kind == matchNone && related[i].kind == matchNone {
			return candidate{}, false
		}
	}
	chosen, inPrimary, total := choose(primary, related, false)
	if total > MAX_TOTAL_EDITS {
		if chosen, inPrimary, total = choose(primary, related, true); total > MAX_TOTAL_EDITS {
			return candidate{}, false
		}
	}
	switch {
	case inPrimary == len(words):
		result.class = CLASS_PRIMARY_ONLY
	case inPrimary > 0:
		result.class = CLASS_PRIMARY_RELATED
	default:
		result.class = CLASS_RELATED_ONLY
	}
	result.edits = total
	for _, m := range chosen {
		result.prefixDependent = result.prefixDependent || m.kind == matchPrefix
	}
	return finish(result, q, item, true), true
}

// choose assigns each word to the primary field when it can match there, which
// raises the class; when the query edit budget is exceeded it minimizes edits.
func choose(primary, related []wordMatch, minimizeEdits bool) (chosen []wordMatch, inPrimary, total int) {
	chosen = make([]wordMatch, len(primary))
	for i := range primary {
		usePrimary := primary[i].kind != matchNone
		if minimizeEdits && usePrimary && related[i].kind != matchNone && related[i].edits < primary[i].edits {
			usePrimary = false
		}
		if usePrimary {
			chosen[i] = primary[i]
			inPrimary++
		} else {
			chosen[i] = related[i]
		}
		total += chosen[i].edits
	}
	return chosen, inPrimary, total
}

// alignedEdits reports whether the query words are the whole primary name, word
// for word, within the per-word and total edit limits.
func alignedEdits(words []string, text *preparedText, limits []int) (int, bool) {
	best, found := 0, false
	for _, primary := range [][]string{text.foldedWords, text.compactWords} {
		if len(primary) != len(words) {
			continue
		}
		total, ok := 0, true
		for i, word := range words {
			if word == primary[i] {
				continue
			}
			edits := editDistance(word, primary[i], limits[i])
			if edits > limits[i] {
				ok = false
				break
			}
			total += edits
		}
		if ok && total <= MAX_TOTAL_EDITS && (!found || total < best) {
			best, found = total, true
		}
	}
	return best, found
}

func finish(result candidate, q query, item *entity, allowPrefix bool) candidate {
	result.faithful = faithfulWords(q.faithful, item, allowPrefix)
	result.versionRank = versionRank(q.versions, item.primary.Versions)
	return result
}

// faithfulWords counts query words whose written form (case-folded, accents
// and punctuation preserved) appears in the record's own fields.
func faithfulWords(queryWords []string, item *entity, allowPrefix bool) int {
	count := 0
	last := len(queryWords) - 1
	for i, word := range queryWords {
		if item.primary.hasFaithful(word, allowPrefix && i == last) {
			count++
			continue
		}
		for _, field := range item.related {
			if field.text.hasFaithful(word, allowPrefix && i == last) {
				count++
				break
			}
		}
	}
	return count
}

func (text *preparedText) hasFaithful(word string, allowPrefix bool) bool {
	for _, candidate := range text.faithfulWords {
		if candidate == word || allowPrefix && strings.HasPrefix(candidate, word) {
			return true
		}
	}
	return false
}

// versionRank prefers the requested allowlisted version, or the plain title
// when no version is requested. It is a bounded ordering criterion only.
func versionRank(requested, stored []string) int {
	if len(requested) == 0 {
		if len(stored) == 0 {
			return 0
		}
		return 1
	}
	for _, version := range requested {
		if !slices.Contains(stored, version) {
			return 1
		}
	}
	return 0
}

func secondaryText(item *entity) string {
	var parts []string
	for _, field := range item.related {
		if _, ok := relatedKinds[field.name]; ok {
			parts = append(parts, field.text.Folded)
		}
	}
	return strings.Join(parts, " / ")
}

type listedEntry struct {
	entity *entity
	match  apigen.LibrarySearchMatch
}

type rankedResults struct {
	bestMatch *listedEntry
	groups    map[string][]listedEntry
}

// rank evaluates strong direct matches first; typo correction runs only when
// no strong direct result exists in any of the five types.
func rank(index *Index, q query) rankedResults {
	if strong := evaluateAll(index, q, false); len(strong) > 0 {
		return assembleStrong(index, strong)
	}
	return assembleCorrected(evaluateAll(index, q, true))
}

func evaluateAll(index *Index, q query, fuzzy bool) []candidate {
	var candidates []candidate
	for _, item := range index.entities {
		if result, ok := evaluate(item, q, fuzzy); ok {
			candidates = append(candidates, result)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].less(candidates[j]) })
	return candidates
}

func assembleStrong(index *Index, candidates []candidate) rankedResults {
	best := listedEntry{entity: candidates[0].entity, match: apigen.Direct}
	byKind := map[string][]candidate{}
	for _, item := range candidates {
		byKind[item.entity.kind] = append(byKind[item.entity.kind], item)
	}
	related := relatedRecords(index, byKind[KIND_TRACK])
	groups := map[string][]listedEntry{}
	for _, kind := range resultKinds {
		groups[kind] = limitGroup(orderGroup(byKind[kind], related[kind]), best.entity)
	}
	return rankedResults{bestMatch: &best, groups: groups}
}

// relatedRecords expands the first five strong Tracks whose titles cover a
// query word into their own Album and their Track and Album Artist credits,
// once, deduplicated by identity and ordered by their strongest source Track.
func relatedRecords(index *Index, tracks []candidate) map[string][]*entity {
	related := map[string][]*entity{}
	seen := map[entityKey]bool{}
	sources := 0
	for _, track := range tracks {
		if track.class > CLASS_PRIMARY_RELATED {
			continue
		}
		if sources++; sources > MAX_RELATED_SOURCES {
			break
		}
		for _, field := range track.entity.related {
			kind, expandable := relatedKinds[field.name]
			if !expandable {
				continue
			}
			key := entityKey{kind, field.relatedID}
			if item := index.lookup(kind, field.relatedID); item != nil && !seen[key] {
				seen[key] = true
				related[kind] = append(related[kind], item)
			}
		}
	}
	return related
}

// orderGroup places exact primary-name matches first, then related records,
// then weaker direct matches; each record appears once at its higher position.
func orderGroup(direct []candidate, related []*entity) []listedEntry {
	var group []listedEntry
	listed := map[string]bool{}
	add := func(item *entity, match apigen.LibrarySearchMatch) {
		if !listed[item.id] {
			listed[item.id] = true
			group = append(group, listedEntry{entity: item, match: match})
		}
	}
	for _, item := range direct {
		if item.class == CLASS_FULL_NAME {
			add(item.entity, apigen.Direct)
		}
	}
	for _, item := range related {
		add(item, apigen.Related)
	}
	for _, item := range direct {
		add(item.entity, apigen.Direct)
	}
	return group
}

func limitGroup(group []listedEntry, exclude *entity) []listedEntry {
	limited := make([]listedEntry, 0, GROUP_LIMIT)
	for _, entry := range group {
		if entry.entity == exclude {
			continue
		}
		if len(limited) == GROUP_LIMIT {
			break
		}
		limited = append(limited, entry)
	}
	return limited
}

func assembleCorrected(candidates []candidate) rankedResults {
	groups := map[string][]listedEntry{}
	for _, item := range candidates {
		if len(groups[item.entity.kind]) < GROUP_LIMIT {
			groups[item.entity.kind] = append(groups[item.entity.kind], listedEntry{entity: item.entity, match: apigen.Corrected})
		}
	}
	return rankedResults{groups: groups}
}
