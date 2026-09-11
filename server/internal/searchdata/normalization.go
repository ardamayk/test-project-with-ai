// Package searchdata prepares derived Library Search fields without changing
// library identity or display metadata. Ranking consumes these representations.
package searchdata

import (
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const RULES_VERSION = 1

// Text retains spelling evidence and two punctuation alternatives. Compact
// joins punctuation within a word, never whitespace-separated words.
type Text struct {
	Original string   `json:"original"`
	Faithful string   `json:"faithful"`
	Folded   string   `json:"folded"`
	Compact  string   `json:"compact"`
	Versions []string `json:"versions"`
}

func PrepareQuery(value string) Text {
	text := prepareText(value)
	text.Versions = findVersions(text.Folded)
	return text
}

func prepareText(value string) Text {
	faithful := strings.Join(strings.Fields(norm.NFC.String(cases.Fold().String(value))), " ")
	return Text{Original: value, Faithful: faithful, Folded: foldWords(faithful, false), Compact: foldWords(faithful, true), Versions: []string{}}
}

func foldWords(value string, compact bool) string {
	var result strings.Builder
	for _, character := range norm.NFD.String(value) {
		switch {
		case unicode.Is(unicode.Mn, character):
			continue
		case character == 'ı':
			result.WriteRune('i')
		case unicode.IsLetter(character) || unicode.IsDigit(character):
			result.WriteRune(character)
		case unicode.IsSpace(character):
			result.WriteByte(' ')
		case character == '\'' || character == '’':
			continue
		case !compact:
			result.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(result.String()), " ")
}

func findVersions(value string) []string {
	versions := []string{}
	words := " " + value + " "
	for _, version := range []string{"live", "remix", "acoustic", "instrumental", "demo", "remaster", "radio edit"} {
		if strings.Contains(words, " "+version+" ") || version == "remaster" && strings.Contains(words, " remastered ") {
			versions = append(versions, version)
		}
	}
	return versions
}
