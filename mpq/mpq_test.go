package mpq

import "testing"

func TestHashConstants(t *testing.T) {
	// Well-known MPQ key values.
	if h := hash("(hash table)", hashTable); h != 0xC3AF3770 {
		t.Fatalf("hash table key = %#x", h)
	}
	if h := hash("(block table)", hashTable); h != 0xEC83B3A3 {
		t.Fatalf("block table key = %#x", h)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("not an archive at all")); err == nil {
		t.Fatal("expected error")
	}
}
