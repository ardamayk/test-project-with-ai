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
)

const (
	MAX_TOTAL_EDITS = 2
	GROUP_LIMIT     = 5
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
	if text.Compact != text.Folded {
		q.variants = append(q.variants, strings.Fields(text.Compact))
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
	for variant, words := range [][]string{text.foldedWords, text.compactWords} {
		if variant == 1 && text.Compact == text.Folded {
			break
		}
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
	if a == b {
		return 0
	}
	if limit == 0 {
		return 1
	}
	left, right := []rune(a), []rune(b)
	if difference := len(left) - len(right); difference > limit || -difference > limit {
		return limit + 1
	}
	// OSA needs only the previous two rows, including for transpositions.
	width := len(right) + 1
	// ponytail: words up to 63 runes stay on the stack; longer words use the same recurrence on the heap.
	var storage [3 * 64]int
	rows := storage[:]
	if 3*width > len(rows) {
		rows = make([]int, 3*width)
	}
	previous, current, older := rows[:width], rows[width:2*width], rows[2*width:3*width]
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(left); i++ {
		current[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			value := min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
			if i > 1 && j > 1 && left[i-1] == right[j-2] && left[i-2] == right[j-1] {
				value = min(value, older[j-2]+1)
			}
			current[j] = value
		}
		older, previous, current = previous, current, older
	}
	return previous[len(right)]
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
	if found {
		best.secondary = secondaryText(item)
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
	result := candidate{entity: item, name: item.primary.Folded}
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
		return candidate{}, false
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
	if minimizeEdits && inPrimary == 0 {
		// Keep the cheapest title/name contribution; related-only is ineligible.
		best := -1
		for i, m := range primary {
			if m.kind != matchNone && (best == -1 || m.edits-chosen[i].edits < primary[best].edits-chosen[best].edits) {
				best = i
			}
		}
		if best != -1 {
			total += primary[best].edits - chosen[best].edits
			chosen[best], inPrimary = primary[best], 1
		}
	}
	return chosen, inPrimary, total
}

// alignedEdits reports whether the query words are the whole primary name, word
// for word, within the per-word and total edit limits.
func alignedEdits(words []string, text *preparedText, limits []int) (int, bool) {
	best, found := 0, false
	for variant, primary := range [][]string{text.foldedWords, text.compactWords} {
		if variant == 1 && text.Compact == text.Folded {
			break
		}
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
		results := assemble(strong, apigen.Direct)
		results.bestMatch = &listedEntry{entity: strong[0].entity, match: apigen.Direct}
		return results
	}
	return assemble(evaluateAll(index, q, true), apigen.Corrected)
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

func assemble(candidates []candidate, match apigen.LibrarySearchMatch) rankedResults {
	groups := map[string][]listedEntry{}
	for _, item := range candidates {
		if len(groups[item.entity.kind]) < GROUP_LIMIT {
			groups[item.entity.kind] = append(groups[item.entity.kind], listedEntry{entity: item.entity, match: match})
		}
	}
	return rankedResults{groups: groups}
}
