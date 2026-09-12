package testutil

// SearchNormalizationCase is shared by data preparation and the future HTTP
// ranking acceptance suite. Expectations are product examples, not computed by
// the normalization implementation.
type SearchNormalizationCase struct {
	Input    string
	Faithful string
	Folded   string
	Compact  string
}

func LibrarySearchNormalizationCases() []SearchNormalizationCase {
	return []SearchNormalizationCase{
		{"  Beyoncé  ", "beyoncé", "beyonce", "beyonce"},
		{"beyonce", "beyonce", "beyonce", "beyonce"},
		{"Şebnem", "şebnem", "sebnem", "sebnem"},
		{"sebnem", "sebnem", "sebnem", "sebnem"},
		{"I İ ı i", "i i̇ ı i", "i i i i", "i i i i"},
		{"don't don’t dont", "don't don’t dont", "dont dont dont", "dont dont dont"},
		{"AC/DC", "ac/dc", "ac dc", "acdc"},
		{"ac dc", "ac dc", "ac dc", "ac dc"},
		{"acdc", "acdc", "acdc", "acdc"},
		{"a b", "a b", "a b", "a b"},
		{"ab", "ab", "ab", "ab"},
		{"東京 １２3", "東京 １２3", "東京 １２3", "東京 １２3"},
		{"... / !", "... / !", "", ""},
	}
}
