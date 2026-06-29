package library

import "testing"

func TestEncodeDecodeGenres(t *testing.T) {
	input := []string{"Pop", "R&B"}
	encoded := encodeGenres(input)
	decoded := decodeGenres(encoded)
	if len(decoded) != 2 || decoded[0] != input[0] || decoded[1] != input[1] {
		t.Fatalf("round trip = %#v, want %#v", decoded, input)
	}

	if got := encodeGenres(nil); got != "[]" {
		t.Fatalf("empty encode = %q", got)
	}
	if got := decodeGenres("[]"); got != nil {
		t.Fatalf("empty decode = %#v", got)
	}
}

func TestSplitGenres(t *testing.T) {
	got := splitGenres("Alternative Pop; Contemporary R&B / Dance-Pop")
	want := []string{"Alternative Pop", "Contemporary R&B", "Dance-Pop"}
	if len(got) != len(want) {
		t.Fatalf("split = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("split[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
