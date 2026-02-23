package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	keySize       = 32 // AES-256
	kekInfo       = "prysm-vault-kek"
	fallbackInfo  = "prysm-vault-fallback-kek"
	compositeInfo = "prysm-vault-composite"
)

// GenerateSalt generates a random 32-byte salt for key derivation.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	return salt, nil
}

// DeriveKEK derives a Key-Encryption Key from a session token and salt using HKDF-SHA256.
func DeriveKEK(token string, salt []byte) ([]byte, error) {
	if token == "" {
		return nil, fmt.Errorf("empty token")
	}
	r := hkdf.New(sha256.New, []byte(token), salt, []byte(kekInfo))
	kek := make([]byte, keySize)
	if _, err := io.ReadFull(r, kek); err != nil {
		return nil, fmt.Errorf("derive KEK: %w", err)
	}
	return kek, nil
}

// DeriveFallbackKEK derives a fallback KEK from salt and user/org identity.
func DeriveFallbackKEK(salt []byte, userID, orgID uint64) ([]byte, error) {
	ikm := fmt.Sprintf("%d:%d", userID, orgID)
	r := hkdf.New(sha256.New, []byte(ikm), salt, []byte(fallbackInfo))
	kek := make([]byte, keySize)
	if _, err := io.ReadFull(r, kek); err != nil {
		return nil, fmt.Errorf("derive fallback KEK: %w", err)
	}
	return kek, nil
}

// GenerateDEK generates a random 32-byte Data Encryption Key.
func GenerateDEK() ([]byte, error) {
	dek := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("generate DEK: %w", err)
	}
	return dek, nil
}

// WrapKey encrypts a key using AES-256-GCM with the given wrapping key.
func WrapKey(wrapKey, plainKey []byte) ([]byte, error) {
	return aesGCMEncrypt(wrapKey, plainKey)
}

// UnwrapKey decrypts a wrapped key using AES-256-GCM with the given wrapping key.
func UnwrapKey(wrapKey, wrappedKey []byte) ([]byte, error) {
	return aesGCMDecrypt(wrapKey, wrappedKey)
}

// KEKFingerprint returns a short hex fingerprint of a KEK for identification.
func KEKFingerprint(kek []byte) string {
	h := sha256.Sum256(kek)
	return hex.EncodeToString(h[:8])
}

// aesGCMEncrypt encrypts plaintext with AES-256-GCM. Output format: nonce || ciphertext || tag.
func aesGCMEncrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// aesGCMDecrypt decrypts AES-256-GCM ciphertext. Input format: nonce || ciphertext || tag.
func aesGCMDecrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize+gcm.Overhead() {
		return nil, ErrInvalidCiphertext
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}

// xchacha20Encrypt encrypts plaintext with XChaCha20-Poly1305. Output format: nonce (24 bytes) || ciphertext || tag.
func xchacha20Encrypt(key, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

// xchacha20Decrypt decrypts XChaCha20-Poly1305 ciphertext. Input format: nonce (24 bytes) || ciphertext || tag.
func xchacha20Decrypt(key, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonceSize := aead.NonceSize()
	if len(ciphertext) < nonceSize+aead.Overhead() {
		return nil, ErrInvalidCiphertext
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}

// DeriveCompositeKey derives a composite key from a classical KEK and a PQC shared secret
// using HKDF-SHA256. The composite key requires breaking both classical and PQC crypto.
func DeriveCompositeKey(kek []byte, pqcSharedSecret [32]byte, salt []byte) ([]byte, error) {
	ikm := make([]byte, len(kek)+len(pqcSharedSecret))
	copy(ikm, kek)
	copy(ikm[len(kek):], pqcSharedSecret[:])
	r := hkdf.New(sha256.New, ikm, salt, []byte(compositeInfo))
	compositeKey := make([]byte, keySize)
	if _, err := io.ReadFull(r, compositeKey); err != nil {
		return nil, fmt.Errorf("derive composite key: %w", err)
	}
	return compositeKey, nil
}
