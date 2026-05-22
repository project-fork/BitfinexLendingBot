package bitfinex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistentNonceGenerator_NextNonceExceedsStoredValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonce.json")

	gen, err := newPersistentNonceGenerator(path)
	if err != nil {
		t.Fatalf("expected no error creating generator, got %v", err)
	}

	first := gen.GetNonce()
	second := gen.GetNonce()

	if !(second > first) {
		t.Fatalf("expected second nonce %q to be greater than first %q", second, first)
	}

	reloaded, err := newPersistentNonceGenerator(path)
	if err != nil {
		t.Fatalf("expected no error reloading generator, got %v", err)
	}

	third := reloaded.GetNonce()
	if !(third > second) {
		t.Fatalf("expected reloaded nonce %q to be greater than prior nonce %q", third, second)
	}
}

func TestPersistentNonceGenerator_RecoversFromInvalidStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonce.json")

	if err := os.WriteFile(path, []byte("{invalid"), 0600); err != nil {
		t.Fatalf("failed to seed invalid nonce file: %v", err)
	}

	gen, err := newPersistentNonceGenerator(path)
	if err != nil {
		t.Fatalf("expected generator to recover from invalid file, got %v", err)
	}

	first := gen.GetNonce()
	second := gen.GetNonce()
	if !(second > first) {
		t.Fatalf("expected monotonic nonces after recovery, got %q then %q", first, second)
	}
}
