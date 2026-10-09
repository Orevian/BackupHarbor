//go:build !windows

package secret

type unsupportedStore struct{}

// NewCredentialManager returns a store that always reports ErrUnsupported.
func NewCredentialManager() Store { return unsupportedStore{} }

func (unsupportedStore) Get(string) (string, error) { return "", ErrUnsupported }
func (unsupportedStore) Set(string, string) error   { return ErrUnsupported }
func (unsupportedStore) Delete(string) error        { return ErrUnsupported }
