package account

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"golang.org/x/crypto/argon2"
)

//go:embed assets/weak-passwords.json
var weakPasswords []byte
var dictionary = sync.OnceValue(func() map[string]bool {
	var rows []string
	if json.Unmarshal(weakPasswords, &rows) != nil {
		panic("ACCOUNT_DICTIONARY_INVALID")
	}
	out := make(map[string]bool, len(rows))
	for _, v := range rows {
		out[v] = true
	}
	return out
})

func ValidatePassword(value []byte) error {
	if len(value) > 512 || !utf8.Valid(value) {
		return field("/password", "PASSWORD_LENGTH")
	}
	r := []rune(string(value))
	if len(r) < 15 || len(r) > 128 {
		return field("/password", "PASSWORD_LENGTH")
	}
	s := string(value)
	lower := strings.ToLower(s)
	if dictionary()[s] || dictionary()[lower] {
		return field("/password", "PASSWORD_WEAK")
	}
	for size := 1; size <= 4; size++ {
		if len(r)%size != 0 {
			continue
		}
		same := true
		for i := size; i < len(r); i++ {
			if r[i] != r[i%size] {
				same = false
				break
			}
		}
		if same {
			return field("/password", "PASSWORD_WEAK")
		}
	}
	for _, sequence := range []string{"0123456789", "9876543210", "abcdefghijklmnopqrstuvwxyz", "zyxwvutsrqponmlkjihgfedcba"} {
		start := strings.IndexByte(sequence, lower[0])
		if start < 0 {
			continue
		}
		same := true
		for i := range len(lower) {
			if lower[i] != sequence[(start+i)%len(sequence)] {
				same = false
				break
			}
		}
		if same {
			return field("/password", "PASSWORD_WEAK")
		}
	}
	return nil
}

const phcPrefix = "$argon2id$v=19$m=65536,t=3,p=4$"
const dummyPHC = phcPrefix + "AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type PasswordHash struct{ data func() string }

func (p PasswordHash) encoded() string {
	if p.data == nil {
		return ""
	}
	return p.data()
}
func (p PasswordHash) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "password_hash") }
func (p PasswordHash) MarshalJSON() ([]byte, error) { return []byte(`"password_hash"`), nil }
func (*PasswordHash) UnmarshalJSON([]byte) error    { return invalid() }
func (p PasswordHash) LogValue() slog.Value         { return slog.StringValue("password_hash") }

// The actual memory budget is process-wide, even with multiple service owners.
var hashCapacity = make(chan struct{}, 18)
var hashSlots = make(chan struct{}, 2)

type hashState struct {
	mu      sync.Mutex
	stopped bool
	active  map[*hashWork]bool
	changed chan struct{}
}
type hashWork struct{ cancel context.CancelFunc }
type PasswordHasher struct{ data func() *hashState }

func NewPasswordHasher() *PasswordHasher {
	s := &hashState{active: map[*hashWork]bool{}, changed: make(chan struct{})}
	return &PasswordHasher{func() *hashState { return s }}
}
func (h *PasswordHasher) withSlot(ctx context.Context, fn func(context.Context) error) error {
	if h == nil || h.data == nil {
		return invalid()
	}
	s := h.data()
	ctx, cancel := context.WithCancel(ctx)
	w := &hashWork{cancel: cancel}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		cancel()
		return fault(foundation.ShuttingDown, nil)
	}
	s.active[w] = true
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.active, w)
		close(s.changed)
		s.changed = make(chan struct{})
		s.mu.Unlock()
	}()
	select {
	case hashCapacity <- struct{}{}:
		defer func() { <-hashCapacity }()
	default:
		return fault(foundation.RateLimited, nil)
	}
	wait, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	case <-wait.Done():
		if ctx.Err() != nil {
			return unavailable(ctx.Err())
		}
		return fault(foundation.RateLimited, nil)
	}
	if ctx.Err() != nil {
		return unavailable(ctx.Err())
	}
	return fn(ctx)
}
func (h *PasswordHasher) Hash(ctx context.Context, password sc.SecretMaterial) (PasswordHash, error) {
	var encoded string
	err := h.withSlot(ctx, func(ctx context.Context) error {
		return password.Use(func(b []byte) error {
			if err := ValidatePassword(b); err != nil {
				return err
			}
			salt := make([]byte, 16)
			if _, err := rand.Read(salt); err != nil {
				return unavailable(err)
			}
			defer clear(salt)
			hash := argon2.IDKey(b, salt, 3, 65536, 4, 32)
			defer clear(hash)
			if ctx.Err() != nil {
				return unavailable(ctx.Err())
			}
			encoded = phcPrefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash)
			return nil
		})
	})
	if err != nil {
		return PasswordHash{}, portError(err)
	}
	return PasswordHash{func() string { return encoded }}, nil
}
func parsePHC(encoded string) ([]byte, []byte, error) {
	if len(encoded) > 256 || !strings.HasPrefix(encoded, phcPrefix) {
		return nil, nil, unavailable(nil)
	}
	parts := strings.Split(strings.TrimPrefix(encoded, phcPrefix), "$")
	if len(parts) != 2 {
		return nil, nil, unavailable(nil)
	}
	salt, e1 := base64.RawStdEncoding.Strict().DecodeString(parts[0])
	hash, e2 := base64.RawStdEncoding.Strict().DecodeString(parts[1])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(hash) != 32 || base64.RawStdEncoding.EncodeToString(salt) != parts[0] || base64.RawStdEncoding.EncodeToString(hash) != parts[1] {
		clear(salt)
		clear(hash)
		return nil, nil, unavailable(nil)
	}
	return salt, hash, nil
}
func (h *PasswordHasher) Verify(ctx context.Context, password sc.SecretMaterial, encoded string) (bool, error) {
	salt, want, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}
	defer clear(salt)
	defer clear(want)
	ok := false
	err = h.withSlot(ctx, func(ctx context.Context) error {
		return password.Use(func(b []byte) error {
			if len(b) > 512 || !utf8.Valid(b) || utf8.RuneCount(b) < 15 || utf8.RuneCount(b) > 128 {
				return field("/password", "PASSWORD_LENGTH")
			}
			got := argon2.IDKey(b, salt, 3, 65536, 4, 32)
			defer clear(got)
			if ctx.Err() != nil {
				return unavailable(ctx.Err())
			}
			ok = subtle.ConstantTimeCompare(got, want) == 1
			return nil
		})
	})
	return ok, portError(err)
}
func (h *PasswordHasher) StopAdmission() { s := h.data(); s.mu.Lock(); s.stopped = true; s.mu.Unlock() }
func (h *PasswordHasher) Drain(ctx context.Context) error {
	s := h.data()
	for {
		s.mu.Lock()
		n, ch := len(s.active), s.changed
		s.mu.Unlock()
		if n == 0 {
			return nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return unavailable(ctx.Err())
		}
	}
}
func (h *PasswordHasher) Force(ctx context.Context) error {
	s := h.data()
	s.mu.Lock()
	s.stopped = true
	for w := range s.active {
		w.cancel()
	}
	s.mu.Unlock()
	return h.Drain(ctx)
}
func (h *PasswordHasher) Joined() bool {
	s := h.data()
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.active) == 0
}
