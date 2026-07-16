package util

import "testing"

func TestSecureToken(t *testing.T) {
	tok, err := SecureToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token")
	}
	if len(tok) != 32 {
		t.Fatalf("expected 32-char hex token, got %d", len(tok))
	}
	// Uniqueness: two consecutive calls should produce different tokens.
	tok2, err := SecureToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok == tok2 {
		t.Fatal("two consecutive SecureToken() calls returned the same value")
	}
}

func TestMustSecureToken(t *testing.T) {
	tok := MustSecureToken()
	if tok == "" || len(tok) != 32 {
		t.Fatalf("MustSecureToken returned invalid token: %q", tok)
	}
}

func TestSecureTokenNoDuplicate(t *testing.T) {
	// Statistical uniqueness check over a batch.
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		tok, err := SecureToken()
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok] {
			t.Fatal("SecureToken produced duplicate value in 100 calls")
		}
		seen[tok] = true
	}
}
