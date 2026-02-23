package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store handles persistence of CLI session state on disk.
type Store struct {
	path string
	mu   sync.RWMutex
}

// Session captures the authentication context cached locally.
type Session struct {
	Token           string        `json:"token,omitempty"`
	RefreshToken    string        `json:"refresh_token,omitempty"`
	TokenEnc        string        `json:"token_enc,omitempty"`
	RefreshTokenEnc string        `json:"refresh_token_enc,omitempty"`
	Email           string        `json:"email"`
	SessionID       string        `json:"session_id"`
	CSRFToken       string        `json:"csrf_token,omitempty"`
	ExpiresAtUnix   int64         `json:"expires_at"`
	SavedAt         time.Time     `json:"saved_at"`
	User            SessionUser   `json:"user"`
	Organization    SessionOrg    `json:"organization"`
	APIBaseURL      string        `json:"api_base_url"`
	ComplianceURL   string        `json:"compliance_url"`
	DERPServerURL   string        `json:"derp_url"`
	PreferredOrg    string        `json:"preferred_org,omitempty"`
	OutputFormat    string        `json:"output_format,omitempty"`
	AdditionalData  interface{}   `json:"additional_data,omitempty"`
	Scopes          []string      `json:"scopes,omitempty"`
	TTLOverride     time.Duration `json:"-"`
}

const encryptedValuePrefix = "enc:v1:"

// SessionUser contains user metadata in the cached session.
type SessionUser struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	MFAEnabled bool   `json:"mfa_enabled"`
}

// SessionOrg contains organization metadata in the cached session.
type SessionOrg struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// NewStore creates a session store writing to the provided path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Path returns the file path used for persistence.
func (s *Store) Path() string {
	return s.path
}

// Load reads the session from disk.
func (s *Store) Load() (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	file, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open session file: %w", err)
	}
	defer file.Close()

	var sess Session
	if err := json.NewDecoder(file).Decode(&sess); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	if sess.TokenEnc != "" || sess.RefreshTokenEnc != "" {
		key, keyErr := s.loadKey()
		if keyErr != nil {
			return nil, fmt.Errorf("load session encryption key: %w", keyErr)
		}
		if sess.Token == "" && sess.TokenEnc != "" {
			plain, decErr := decryptString(key, sess.TokenEnc)
			if decErr != nil {
				return nil, fmt.Errorf("decrypt session token: %w", decErr)
			}
			sess.Token = plain
		}
		if sess.RefreshToken == "" && sess.RefreshTokenEnc != "" {
			plain, decErr := decryptString(key, sess.RefreshTokenEnc)
			if decErr != nil {
				return nil, fmt.Errorf("decrypt session refresh token: %w", decErr)
			}
			sess.RefreshToken = plain
		}
	}

	if sess.SavedAt.IsZero() {
		// Backfill using file metadata
		if info, statErr := file.Stat(); statErr == nil {
			sess.SavedAt = info.ModTime()
		} else {
			sess.SavedAt = time.Now()
		}
	}

	return &sess, nil
}

// Save persists the session to disk with restrictive permissions.
func (s *Store) Save(sess *Session) error {
	if sess == nil {
		return errors.New("session is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("ensure session directory: %w", err)
	}

	sess.SavedAt = time.Now()
	key, err := s.getOrCreateKey()
	if err != nil {
		return fmt.Errorf("get session encryption key: %w", err)
	}

	persist := *sess
	if persist.Token != "" {
		enc, encErr := encryptString(key, persist.Token)
		if encErr != nil {
			return fmt.Errorf("encrypt session token: %w", encErr)
		}
		persist.TokenEnc = enc
		persist.Token = ""
	}
	if persist.RefreshToken != "" {
		enc, encErr := encryptString(key, persist.RefreshToken)
		if encErr != nil {
			return fmt.Errorf("encrypt session refresh token: %w", encErr)
		}
		persist.RefreshTokenEnc = enc
		persist.RefreshToken = ""
	}

	tempFile := s.path + ".tmp"
	file, err := os.OpenFile(tempFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create temp session: %w", err)
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(&persist); err != nil {
		file.Close()
		return fmt.Errorf("write session: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close session: %w", err)
	}

	if err := os.Rename(tempFile, s.path); err != nil {
		return fmt.Errorf("atomically replace session file: %w", err)
	}

	return nil
}

func (s *Store) keyPath() string {
	return s.path + ".key"
}

func (s *Store) loadKey() ([]byte, error) {
	key, err := os.ReadFile(s.keyPath())
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key length %d", len(key))
	}
	return key, nil
}

func (s *Store) getOrCreateKey() ([]byte, error) {
	key, err := s.loadKey()
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	key = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	tempPath := s.keyPath() + ".tmp"
	if err := os.WriteFile(tempPath, key, 0o600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	if err := os.Rename(tempPath, s.keyPath()); err != nil {
		return nil, fmt.Errorf("persist key: %w", err)
	}

	return key, nil
}

func encryptString(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	raw := append(nonce, ciphertext...)
	return encryptedValuePrefix + base64.StdEncoding.EncodeToString(raw), nil
}

func decryptString(key []byte, value string) (string, error) {
	if !strings.HasPrefix(value, encryptedValuePrefix) {
		return value, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encryptedValuePrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// Clear removes the session file from disk.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove session: %w", err)
	}
	return nil
}

// ExpiresAt returns the session expiration timestamp.
func (s *Session) ExpiresAt() time.Time {
	if s == nil {
		return time.Time{}
	}
	if s.TTLOverride > 0 {
		return s.SavedAt.Add(s.TTLOverride)
	}
	if s.ExpiresAtUnix > 0 {
		return time.Unix(s.ExpiresAtUnix, 0)
	}
	return time.Time{}
}

// IsExpired returns true if the session is expired or within the provided window.
func (s *Session) IsExpired(window time.Duration) bool {
	exp := s.ExpiresAt()
	if exp.IsZero() {
		return false
	}
	if window < 0 {
		window = 0
	}
	return time.Now().After(exp.Add(-window))
}
