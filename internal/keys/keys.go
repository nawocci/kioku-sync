// Package keys generates and verifies API keys. Keys are 256-bit random
// values presented in a typed base64url form; only their SHA-256 hashes are
// stored. Keys are minted from the server console only.
package keys

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// KeyPrefix marks API keys so they are recognizable when pasted.
const KeyPrefix = "kioku_"

// ErrDuplicateLabel reports a library label already in use.
var ErrDuplicateLabel = errors.New("label already in use")

// Generate returns a new API key string.
func Generate() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return KeyPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// Hash returns the storage form of a key.
func Hash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// CreateKey creates a new library and returns its API key. An empty label
// defaults to Key-N for the Nth library, so devices pair into a recognizable
// sequence without anyone naming them.
func CreateKey(db *sql.DB, label string) (string, error) {
	if label == "" {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM libraries`).Scan(&count); err != nil {
			return "", err
		}
		label = fmt.Sprintf("Key-%d", count+1)
	}

	key, err := Generate()
	if err != nil {
		return "", err
	}
	_, err = db.Exec(
		`INSERT INTO libraries (label, key_hash, created_at, last_seq) VALUES (?, ?, strftime('%s','now'), 0)`,
		label, Hash(key),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: libraries.label") {
			return "", ErrDuplicateLabel
		}
		return "", err
	}
	return key, nil
}

// Lookup returns the library id of an API key, or 0 when unknown.
func Lookup(db *sql.DB, key string) (int64, string, error) {
	if !strings.HasPrefix(key, KeyPrefix) {
		return 0, "", fmt.Errorf("not a %s key", KeyPrefix)
	}
	var id int64
	var label string
	err := db.QueryRow(`SELECT id, label FROM libraries WHERE key_hash = ?`, Hash(key)).
		Scan(&id, &label)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	return id, label, err
}
