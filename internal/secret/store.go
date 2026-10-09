package secret

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Password source kinds understood by the configuration.
const (
	SourceCredential = "credential"
	SourceEnv        = "env"
	SourceFile       = "file"
)

// CredentialPrefix namespaces every Credential Manager entry owned by BackupHarbor.
const CredentialPrefix = "BackupHarbor/"

// ErrNotFound is returned when a credential does not exist.
var ErrNotFound = errors.New("credential not found")

// ErrUnsupported is returned on platforms without Credential Manager.
var ErrUnsupported = errors.New("credential storage is only available on Windows")

// Store is a secret store keyed by credential name.
type Store interface {
	Get(name string) (string, error)
	Set(name, value string) error
	Delete(name string) error
}

// maxPasswordFileSize bounds password files so a wrong path cannot load a large file.
const maxPasswordFileSize = 4096

// Source describes where a repository password comes from.
type Source struct {
	Kind       string
	Credential string // credential name (without prefix)
	Env        string // environment variable name
	File       string // absolute path of a password file
}

// Resolve loads the password. The returned value must never be logged.
func Resolve(src Source, store Store) (string, error) {
	switch src.Kind {
	case SourceCredential:
		if store == nil {
			return "", ErrUnsupported
		}
		v, err := store.Get(CredentialPrefix + src.Credential)
		if err != nil {
			return "", err
		}
		if v == "" {
			return "", ErrNotFound
		}
		return v, nil
	case SourceEnv:
		v, ok := os.LookupEnv(src.Env)
		if !ok || v == "" {
			return "", fmt.Errorf("environment variable %s is not set or empty", src.Env)
		}
		return v, nil
	case SourceFile:
		fi, err := os.Stat(src.File)
		if err != nil {
			return "", err
		}
		if fi.IsDir() {
			return "", fmt.Errorf("%s is a directory", src.File)
		}
		if fi.Size() > maxPasswordFileSize {
			return "", fmt.Errorf("password file is larger than %d bytes", maxPasswordFileSize)
		}
		b, err := os.ReadFile(src.File)
		if err != nil {
			return "", err
		}
		v := strings.TrimRight(string(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})), string([]rune{13, 10}))
		if v == "" {
			return "", errors.New("password file is empty")
		}
		return v, nil
	}
	return "", fmt.Errorf("unknown password source %q", src.Kind)
}

// MemoryStore is an in-memory Store used by tests.
type MemoryStore struct{ m map[string]string }

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore { return &MemoryStore{m: map[string]string{}} }

// Get implements Store.
func (s *MemoryStore) Get(name string) (string, error) {
	v, ok := s.m[name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// Set implements Store.
func (s *MemoryStore) Set(name, value string) error { s.m[name] = value; return nil }

// Delete implements Store.
func (s *MemoryStore) Delete(name string) error {
	if _, ok := s.m[name]; !ok {
		return ErrNotFound
	}
	delete(s.m, name)
	return nil
}
