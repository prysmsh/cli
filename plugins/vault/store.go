package vault

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

// Store wraps a bbolt database and provides encrypted storage operations.
type Store struct {
	db  *bolt.DB
	dek []byte
}

// OpenStore opens or creates the bbolt database at the given path.
func OpenStore(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create vault directory: %w", err)
	}
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("open vault database: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// SetDEK sets the data encryption key used by encrypted operations.
func (s *Store) SetDEK(dek []byte) {
	s.dek = dek
}

// IsInitialized checks whether the vault has been initialized.
func (s *Store) IsInitialized() bool {
	var ok bool
	_ = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("meta"))
		if b != nil && b.Get([]byte("wrapped_dek")) != nil {
			ok = true
		}
		return nil
	})
	return ok
}

// PutMeta stores a value in the meta bucket (unencrypted).
func (s *Store) PutMeta(key string, value []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("meta"))
		if err != nil {
			return err
		}
		return b.Put([]byte(key), value)
	})
}

// GetMeta retrieves a value from the meta bucket (unencrypted).
func (s *Store) GetMeta(key string) ([]byte, error) {
	var val []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("meta"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(key))
		if v != nil {
			val = make([]byte, len(v))
			copy(val, v)
		}
		return nil
	})
	return val, err
}

// PutEncrypted encrypts plaintext with the DEK and stores it.
func (s *Store) PutEncrypted(bucket, key string, plaintext []byte) error {
	ct, err := xchacha20Encrypt(s.dek, plaintext)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		return b.Put([]byte(key), ct)
	})
}

// GetEncrypted retrieves and decrypts a value using the DEK.
func (s *Store) GetEncrypted(bucket, key string) ([]byte, error) {
	var ct []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(key))
		if v != nil {
			ct = make([]byte, len(v))
			copy(ct, v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if ct == nil {
		return nil, nil
	}
	return xchacha20Decrypt(s.dek, ct)
}

// Put stores a raw (unencrypted) value in the given bucket.
func (s *Store) Put(bucket, key string, value []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		return b.Put([]byte(key), value)
	})
}

// Get retrieves a raw (unencrypted) value from the given bucket.
func (s *Store) Get(bucket, key string) ([]byte, error) {
	var val []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(key))
		if v != nil {
			val = make([]byte, len(v))
			copy(val, v)
		}
		return nil
	})
	return val, err
}

// Delete removes a key from a bucket.
func (s *Store) Delete(bucket, key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

// List returns all keys in a bucket, optionally filtered by prefix.
func (s *Store) List(bucket, prefix string) ([]string, error) {
	var keys []string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		if prefix == "" {
			for k, _ := c.First(); k != nil; k, _ = c.Next() {
				keys = append(keys, string(k))
			}
		} else {
			pfx := []byte(prefix)
			for k, _ := c.Seek(pfx); k != nil && bytesHasPrefix(k, pfx); k, _ = c.Next() {
				keys = append(keys, string(k))
			}
		}
		return nil
	})
	return keys, err
}

// AppendSequential stores a value with an auto-incrementing uint64 key (big-endian).
func (s *Store) AppendSequential(bucket string, value []byte) (uint64, error) {
	var seq uint64
	err := s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		seq, err = b.NextSequence()
		if err != nil {
			return err
		}
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, seq)
		return b.Put(key, value)
	})
	return seq, err
}

// ScanSequential iterates over sequential entries in a bucket, calling fn for each.
// Entries are visited in ascending order. Return a non-nil error from fn to stop.
func (s *Store) ScanSequential(bucket string, fn func(seq uint64, value []byte) error) error {
	return s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if len(k) != 8 {
				continue
			}
			seq := binary.BigEndian.Uint64(k)
			val := make([]byte, len(v))
			copy(val, v)
			if err := fn(seq, val); err != nil {
				return err
			}
		}
		return nil
	})
}

// CountKeys returns the number of keys in a bucket.
func (s *Store) CountKeys(bucket string) int {
	var count int
	_ = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		count = b.Stats().KeyN
		return nil
	})
	return count
}

func bytesHasPrefix(s, prefix []byte) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i, b := range prefix {
		if s[i] != b {
			return false
		}
	}
	return true
}
