package wg

import (
	"crypto/hkdf"
	"crypto/mlkem"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const pskInfo = "prysm-wg-psk-v1"

// EnsureMLKEMKeyPair loads or generates an ML-KEM-768 decapsulation key.
// Returns the decapsulation key and the base64-encoded encapsulation key (public).
func EnsureMLKEMKeyPair(homeDir string) (*mlkem.DecapsulationKey768, string, error) {
	keyPath := filepath.Join(homeDir, "prysm0.mlkem.key")

	if data, err := os.ReadFile(keyPath); err == nil {
		raw, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if decodeErr == nil {
			dk, parseErr := mlkem.NewDecapsulationKey768(raw)
			if parseErr == nil {
				pub := base64.StdEncoding.EncodeToString(dk.EncapsulationKey().Bytes())
				return dk, pub, nil
			}
		}
	}

	dk, err := mlkem.GenerateKey768()
	if err != nil {
		return nil, "", fmt.Errorf("generate ml-kem key: %w", err)
	}

	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create key dir: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(dk.Bytes())
	if err := os.WriteFile(keyPath, []byte(encoded+"\n"), 0o600); err != nil {
		return nil, "", fmt.Errorf("write ml-kem key: %w", err)
	}

	pub := base64.StdEncoding.EncodeToString(dk.EncapsulationKey().Bytes())
	return dk, pub, nil
}

// Encapsulate encapsulates to peerMLKEMPubKeyB64 and returns (ciphertext_base64, shared_secret_bytes).
func Encapsulate(peerMLKEMPubKeyB64 string) (ciphertextB64 string, sharedSecret []byte, err error) {
	raw, err := base64.StdEncoding.DecodeString(peerMLKEMPubKeyB64)
	if err != nil {
		return "", nil, fmt.Errorf("decode peer ml-kem public key: %w", err)
	}
	ek, err := mlkem.NewEncapsulationKey768(raw)
	if err != nil {
		return "", nil, fmt.Errorf("parse peer ml-kem public key: %w", err)
	}
	ct, ss := ek.Encapsulate()
	return base64.StdEncoding.EncodeToString(ct), ss, nil
}

// Decapsulate decapsulates a ciphertext using our ML-KEM decapsulation key
// and returns the shared secret bytes.
func Decapsulate(dk *mlkem.DecapsulationKey768, ciphertextB64 string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("decode ml-kem ciphertext: %w", err)
	}
	ss, err := dk.Decapsulate(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decapsulate: %w", err)
	}
	return ss, nil
}

// DeriveBilateralPSK derives a 32-byte WireGuard PSK from two ML-KEM shared secrets.
//
// ssOurs is the shared secret from our encapsulation to the peer.
// ssPeer is the shared secret from the peer's encapsulation to us (nil if not yet available).
//
// Both peers compute the same PSK because:
//   - Our ss comes from our KEM to their pubkey; they decapsulate to get the same ss.
//   - Their ss comes from their KEM to our pubkey; we decapsulate to get the same ss.
//   - Inputs are ordered by WG public key so both sides use identical HKDF input.
//
// If ssPeer is nil, falls back to a one-sided PSK (still quantum-resistant against
// peer-impersonation; upgrades to bilateral on next reconnect once both ciphertexts exist).
func DeriveBilateralPSK(ssOurs, ssPeer []byte, ourWGPub, peerWGPub string) (string, error) {
	// Deterministic ordering: the device with the smaller WG pubkey contributes ss first.
	var ikm []byte
	if ssPeer == nil {
		// One-sided fallback: only our encapsulation has landed so far.
		ikm = ssOurs
	} else if ourWGPub < peerWGPub {
		// We are "A" (lower): ikm = ss_A_to_B || ss_B_to_A
		ikm = append(append([]byte{}, ssOurs...), ssPeer...)
	} else {
		// We are "B" (higher): peer is A, ikm = ss_A_to_B || ss_B_to_A
		// ssOurs = ss_B_to_A, ssPeer = ss_A_to_B → swap to canonical order.
		ikm = append(append([]byte{}, ssPeer...), ssOurs...)
	}

	// Salt binds the PSK to this specific peer pair.
	salt := ourWGPub + "|" + peerWGPub
	if peerWGPub < ourWGPub {
		salt = peerWGPub + "|" + ourWGPub
	}

	psk, err := hkdf.Key(sha256.New, ikm, []byte(salt), pskInfo, 32)
	if err != nil {
		return "", fmt.Errorf("derive psk: %w", err)
	}
	return fmt.Sprintf("%x", psk), nil
}
