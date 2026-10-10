//go:build !linux && !darwin

package identity

// This implementation deliberately has no fallback to weaker file ownership or
// locking semantics. D15 supports only its independently verified Unix targets.
type LockedFile struct{}

func Open(string) (*LockedFile, error)      { return nil, ErrUnsupported }
func ReadOnly(string) (Identity, error)     { return Identity{}, ErrUnsupported }
func (*LockedFile) Load() (Identity, error) { return Identity{}, ErrUnsupported }
func (*LockedFile) Save(Identity) error     { return ErrUnsupported }
func (*LockedFile) Close() error            { return nil }
