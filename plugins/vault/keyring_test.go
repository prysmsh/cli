package vault

import (
	"bytes"
	"testing"
)

func TestGenerateSalt(t *testing.T) {
	s1, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	if len(s1) != 32 {
		t.Fatalf("expected 32-byte salt, got %d", len(s1))
	}

	s2, _ := GenerateSalt()
	if bytes.Equal(s1, s2) {
		t.Fatal("two salts should not be equal")
	}
}

func TestDeriveKEK(t *testing.T) {
	salt, _ := GenerateSalt()

	kek1, err := DeriveKEK("token-a", salt)
	if err != nil {
		t.Fatal(err)
	}
	if len(kek1) != 32 {
		t.Fatalf("expected 32-byte KEK, got %d", len(kek1))
	}

	// Same inputs produce same output.
	kek2, _ := DeriveKEK("token-a", salt)
	if !bytes.Equal(kek1, kek2) {
		t.Fatal("same token+salt should produce same KEK")
	}

	// Different token produces different KEK.
	kek3, _ := DeriveKEK("token-b", salt)
	if bytes.Equal(kek1, kek3) {
		t.Fatal("different tokens should produce different KEKs")
	}

	// Empty token should error.
	_, err = DeriveKEK("", salt)
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestDeriveFallbackKEK(t *testing.T) {
	salt, _ := GenerateSalt()

	fb1, err := DeriveFallbackKEK(salt, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(fb1) != 32 {
		t.Fatalf("expected 32-byte KEK, got %d", len(fb1))
	}

	// Same inputs produce same output.
	fb2, _ := DeriveFallbackKEK(salt, 1, 100)
	if !bytes.Equal(fb1, fb2) {
		t.Fatal("same inputs should produce same fallback KEK")
	}

	// Different identity produces different KEK.
	fb3, _ := DeriveFallbackKEK(salt, 2, 100)
	if bytes.Equal(fb1, fb3) {
		t.Fatal("different userID should produce different fallback KEK")
	}
}

func TestGenerateDEK(t *testing.T) {
	dek, err := GenerateDEK()
	if err != nil {
		t.Fatal(err)
	}
	if len(dek) != 32 {
		t.Fatalf("expected 32-byte DEK, got %d", len(dek))
	}
}

func TestWrapUnwrapKey(t *testing.T) {
	kek, _ := GenerateDEK()
	dek, _ := GenerateDEK()

	wrapped, err := WrapKey(kek, dek)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(wrapped, dek) {
		t.Fatal("wrapped key should differ from plaintext")
	}

	unwrapped, err := UnwrapKey(kek, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unwrapped, dek) {
		t.Fatal("unwrapped key should equal original")
	}

	// Wrong KEK should fail.
	wrongKEK, _ := GenerateDEK()
	_, err = UnwrapKey(wrongKEK, wrapped)
	if err == nil {
		t.Fatal("expected error with wrong KEK")
	}
}

func TestKEKFingerprint(t *testing.T) {
	kek, _ := GenerateDEK()
	fp := KEKFingerprint(kek)
	if len(fp) != 16 { // 8 bytes hex
		t.Fatalf("expected 16-char fingerprint, got %d: %s", len(fp), fp)
	}

	// Same input produces same fingerprint.
	fp2 := KEKFingerprint(kek)
	if fp != fp2 {
		t.Fatal("same KEK should produce same fingerprint")
	}
}

func TestAESGCMRoundtrip(t *testing.T) {
	key, _ := GenerateDEK()
	plaintext := []byte("hello world, this is a secret message")

	ct, err := aesGCMEncrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ct, plaintext) {
		t.Fatal("ciphertext should differ from plaintext")
	}

	pt, err := aesGCMDecrypt(key, ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatal("decrypted text should equal original")
	}
}

func TestAESGCMDecryptInvalidCiphertext(t *testing.T) {
	key, _ := GenerateDEK()

	_, err := aesGCMDecrypt(key, []byte("short"))
	if err != ErrInvalidCiphertext {
		t.Fatalf("expected ErrInvalidCiphertext, got %v", err)
	}
}

func TestXChaCha20Roundtrip(t *testing.T) {
	key, _ := GenerateDEK()
	plaintext := []byte("post-quantum defense in depth")

	ct, err := xchacha20Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ct, plaintext) {
		t.Fatal("ciphertext should differ from plaintext")
	}

	pt, err := xchacha20Decrypt(key, ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatal("decrypted text should equal original")
	}
}

func TestXChaCha20DecryptInvalidCiphertext(t *testing.T) {
	key, _ := GenerateDEK()

	_, err := xchacha20Decrypt(key, []byte("short"))
	if err != ErrInvalidCiphertext {
		t.Fatalf("expected ErrInvalidCiphertext, got %v", err)
	}
}

func TestXChaCha20DecryptWrongKey(t *testing.T) {
	key1, _ := GenerateDEK()
	key2, _ := GenerateDEK()

	ct, err := xchacha20Encrypt(key1, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = xchacha20Decrypt(key2, ct)
	if err != ErrDecryptionFailed {
		t.Fatalf("expected ErrDecryptionFailed, got %v", err)
	}
}

func TestDeriveCompositeKeyDeterministic(t *testing.T) {
	kek, _ := GenerateDEK()
	salt, _ := GenerateSalt()
	var secret [32]byte
	copy(secret[:], []byte("test-shared-secret-32-bytes-long"))

	ck1, err := DeriveCompositeKey(kek, secret, salt)
	if err != nil {
		t.Fatal(err)
	}
	if len(ck1) != 32 {
		t.Fatalf("expected 32-byte composite key, got %d", len(ck1))
	}

	// Same inputs produce same key.
	ck2, _ := DeriveCompositeKey(kek, secret, salt)
	if !bytes.Equal(ck1, ck2) {
		t.Fatal("same inputs should produce same composite key")
	}

	// Different KEK produces different key.
	kek2, _ := GenerateDEK()
	ck3, _ := DeriveCompositeKey(kek2, secret, salt)
	if bytes.Equal(ck1, ck3) {
		t.Fatal("different KEK should produce different composite key")
	}

	// Different shared secret produces different key.
	var secret2 [32]byte
	copy(secret2[:], []byte("different-secret-32-bytes-value!"))
	ck4, _ := DeriveCompositeKey(kek, secret2, salt)
	if bytes.Equal(ck1, ck4) {
		t.Fatal("different shared secret should produce different composite key")
	}

	// Composite key differs from plain KEK.
	if bytes.Equal(ck1, kek) {
		t.Fatal("composite key should differ from plain KEK")
	}
}
