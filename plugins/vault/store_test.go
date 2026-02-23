package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestStoreOpenClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")

	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// File should exist.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("vault.db should exist after OpenStore")
	}
}

func TestStoreCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "vault.db")

	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("vault.db should exist in nested directory")
	}
}

func TestStoreIsInitialized(t *testing.T) {
	store := tempStore(t)

	if store.IsInitialized() {
		t.Fatal("new store should not be initialized")
	}

	if err := store.PutMeta("wrapped_dek", []byte("fake")); err != nil {
		t.Fatal(err)
	}

	if !store.IsInitialized() {
		t.Fatal("store should be initialized after setting wrapped_dek")
	}
}

func TestStoreMetaRoundtrip(t *testing.T) {
	store := tempStore(t)

	if err := store.PutMeta("key1", []byte("value1")); err != nil {
		t.Fatal(err)
	}

	val, err := store.GetMeta("key1")
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "value1" {
		t.Fatalf("expected value1, got %s", val)
	}

	// Non-existent key.
	val, err = store.GetMeta("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if val != nil {
		t.Fatalf("expected nil for nonexistent key, got %s", val)
	}
}

func TestStorePutGetRaw(t *testing.T) {
	store := tempStore(t)

	if err := store.Put("mybucket", "k1", []byte("v1")); err != nil {
		t.Fatal(err)
	}

	val, err := store.Get("mybucket", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "v1" {
		t.Fatalf("expected v1, got %s", val)
	}
}

func TestStoreEncryptedRoundtrip(t *testing.T) {
	store := tempStore(t)
	dek, _ := GenerateDEK()
	store.SetDEK(dek)

	plaintext := []byte("secret data here")
	if err := store.PutEncrypted("secrets", "mykey", plaintext); err != nil {
		t.Fatal(err)
	}

	// Raw read should give ciphertext, not plaintext.
	raw, _ := store.Get("secrets", "mykey")
	if bytes.Equal(raw, plaintext) {
		t.Fatal("raw stored value should be encrypted")
	}

	// Encrypted read should give plaintext.
	decrypted, err := store.GetEncrypted("secrets", "mykey")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted value should equal original")
	}
}

func TestStoreDelete(t *testing.T) {
	store := tempStore(t)

	store.Put("b", "k1", []byte("v1"))
	store.Delete("b", "k1")

	val, _ := store.Get("b", "k1")
	if val != nil {
		t.Fatal("deleted key should return nil")
	}
}

func TestStoreList(t *testing.T) {
	store := tempStore(t)

	store.Put("b", "alpha", []byte("1"))
	store.Put("b", "beta", []byte("2"))
	store.Put("b", "gamma", []byte("3"))

	// All keys.
	keys, err := store.List("b", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}

	// Prefix filter.
	keys, err = store.List("b", "al")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "alpha" {
		t.Fatalf("expected [alpha], got %v", keys)
	}

	// Non-existent bucket.
	keys, _ = store.List("nonexistent", "")
	if len(keys) != 0 {
		t.Fatal("non-existent bucket should return empty list")
	}
}

func TestStoreSequentialAppend(t *testing.T) {
	store := tempStore(t)

	seq1, err := store.AppendSequential("log", []byte("entry1"))
	if err != nil {
		t.Fatal(err)
	}
	seq2, err := store.AppendSequential("log", []byte("entry2"))
	if err != nil {
		t.Fatal(err)
	}
	if seq2 <= seq1 {
		t.Fatalf("sequences should increase: %d <= %d", seq2, seq1)
	}

	var entries []string
	err = store.ScanSequential("log", func(seq uint64, value []byte) error {
		entries = append(entries, string(value))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0] != "entry1" || entries[1] != "entry2" {
		t.Fatalf("unexpected entries: %v", entries)
	}
}

func TestStoreCountKeys(t *testing.T) {
	store := tempStore(t)

	if store.CountKeys("empty") != 0 {
		t.Fatal("empty bucket should have 0 keys")
	}

	store.Put("b", "k1", []byte("v1"))
	store.Put("b", "k2", []byte("v2"))

	if store.CountKeys("b") != 2 {
		t.Fatalf("expected 2, got %d", store.CountKeys("b"))
	}
}
