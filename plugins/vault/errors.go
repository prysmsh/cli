package vault

import "errors"

var (
	ErrVaultNotInitialized     = errors.New("vault not initialized — run `prysm vault init` first")
	ErrVaultAlreadyInitialized = errors.New("vault is already initialized")
	ErrKeyNotFound             = errors.New("key not found")
	ErrKeyAlreadyExists        = errors.New("key already exists")
	ErrDecryptionFailed        = errors.New("decryption failed")
	ErrInvalidCiphertext       = errors.New("invalid ciphertext format")
	ErrCANotInitialized        = errors.New("CA not initialized — run `prysm vault pki init-ca` first")
	ErrCertificateNotFound     = errors.New("certificate not found")
	ErrCertificateRevoked      = errors.New("certificate has been revoked")
	ErrInvalidArguments        = errors.New("invalid arguments")
	ErrSecretNotFound          = errors.New("secret not found")
	ErrUnsupportedAlgorithm    = errors.New("unsupported algorithm")
)
