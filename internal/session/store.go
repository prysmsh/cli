package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Token          string        `json:"token"`
	RefreshToken   string        `json:"refresh_token,omitempty"`
	Email          string        `json:"email"`
	SessionID      string        `json:"session_id"`
	CSRFToken      string        `json:"csrf_token,omitempty"`
	ExpiresAtUnix  int64         `json:"expires_at"`
	SavedAt        time.Time     `json:"saved_at"`
	User           SessionUser   `json:"user"`
	Organization   SessionOrg    `json:"organization"`
	APIBaseURL     string        `json:"api_base_url"`
	ComplianceURL  string        `json:"compliance_url"`
	DERPServerURL  string        `json:"derp_url"`
	PreferredOrg   string        `json:"preferred_org,omitempty"`
	OutputFormat   string        `json:"output_format,omitempty"`
	AdditionalData interface{}   `json:"additional_data,omitempty"`
	Scopes         []string      `json:"scopes,omitempty"`
	TTLOverride    time.Duration `json:"-"`
}

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

	tempFile := s.path + ".tmp"
	file, err := os.OpenFile(tempFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create temp session: %w", err)
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(sess); err != nil {
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
