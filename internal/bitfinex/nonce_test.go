package bitfinex

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
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

func TestPersistentNonceGenerator_UsesMicrosecondEpochScale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonce.json")

	gen, err := newPersistentNonceGenerator(path)
	if err != nil {
		t.Fatalf("expected no error creating generator, got %v", err)
	}

	lowerBound := uint64(time.Now().Unix()) * 1_000_000
	firstRaw := gen.GetNonce()
	upperBound := uint64(time.Now().Unix()+1) * 1_000_000

	first, err := strconv.ParseUint(firstRaw, 10, 64)
	if err != nil {
		t.Fatalf("expected numeric nonce, got %q: %v", firstRaw, err)
	}

	if first < lowerBound || first > upperBound {
		t.Fatalf("expected first nonce %d to use microsecond epoch scale within [%d, %d]", first, lowerBound, upperBound)
	}
}

func TestPersistentNonceGenerator_ResetsInvalidNanosecondScaleState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonce.json")

	invalidState := []byte("{\n  \"last_nonce\": 1779433757242774350\n}\n")
	if err := os.WriteFile(path, invalidState, 0600); err != nil {
		t.Fatalf("failed to seed invalid nonce state: %v", err)
	}

	gen, err := newPersistentNonceGenerator(path)
	if err != nil {
		t.Fatalf("expected no error creating generator, got %v", err)
	}

	lowerBound := uint64(time.Now().Unix()) * 1_000_000
	raw := gen.GetNonce()
	upperBound := uint64(time.Now().Unix()+1) * 1_000_000

	got, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		t.Fatalf("expected numeric nonce, got %q: %v", raw, err)
	}

	if got < lowerBound || got > upperBound {
		t.Fatalf("expected recovered nonce %d to be corrected into microsecond epoch range [%d, %d]", got, lowerBound, upperBound)
	}
}
