// Package runnerprotocol owns the bounded v1.0 wire shared by Central and Runner.
// It deliberately has no dependency on Central identities or business packages.
package runnerprotocol

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"
)

const (
	MaxMessageBytes         = 1 << 20
	MaxJSONDepth            = 16
	MaxJSONMembers          = 4096
	MaxQueuedMessages       = 256
	MaxQueuedBytes          = 4 << 20
	ReservedControlMessages = 32
	ReservedControlBytes    = 256 << 10
	MaxPendingRequests      = 4096
	MaxSeenMessages         = 8192
	MaxStreamBytes          = 16 << 10
	Subprotocol             = "agenteam.runner.v1"
)

var (
	ErrInvalid             = errors.New("invalid runner protocol message")
	ErrTooLarge            = errors.New("runner protocol limit exceeded")
	ErrCorrelation         = errors.New("invalid runner protocol correlation")
	ErrIncompatibleVersion = errors.New("incompatible runner protocol version")
	ErrDirection           = errors.New("invalid runner protocol direction")
)

type ID string

func (v ID) Valid() bool {
	s := string(v)
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' || s[14] != '7' || !(s[19] == '8' || s[19] == '9' || s[19] == 'a' || s[19] == 'b') {
		return false
	}
	for i := range len(s) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !lowerHex(s[i]) {
			return false
		}
	}
	return true
}
func NewID() (ID, error) {
	var b [16]byte
	ms := time.Now().UnixMilli()
	if ms < 0 || ms > 1<<48-1 {
		return "", ErrInvalid
	}
	if _, err := rand.Read(b[6:]); err != nil {
		return "", errors.New("runner identity entropy unavailable")
	}
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	b[6] = b[6]&15 | 0x70
	b[8] = b[8]&63 | 0x80
	var s [36]byte
	hex.Encode(s[:8], b[:4])
	s[8] = '-'
	hex.Encode(s[9:13], b[4:6])
	s[13] = '-'
	hex.Encode(s[14:18], b[6:8])
	s[18] = '-'
	hex.Encode(s[19:23], b[8:10])
	s[23] = '-'
	hex.Encode(s[24:], b[10:])
	return ID(s[:]), nil
}
func lowerHex(b byte) bool { return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' }

type Instant string

func (v Instant) Valid() bool {
	s := string(v)
	if len(s) < 20 || len(s) > 30 || s[len(s)-1] != 'Z' {
		return false
	}
	t, e := time.Parse(time.RFC3339Nano, s)
	return e == nil && t.UTC().Format(time.RFC3339Nano) == s
}
func NewInstant(t time.Time) (Instant, error) {
	s := Instant(t.UTC().Format(time.RFC3339Nano))
	if !s.Valid() {
		return "", ErrInvalid
	}
	return s, nil
}
func (v Instant) Time() (time.Time, error) {
	if !v.Valid() {
		return time.Time{}, ErrInvalid
	}
	return time.Parse(time.RFC3339Nano, string(v))
}

type Version struct {
	Major uint16 `json:"major"`
	Minor uint16 `json:"minor"`
}

func CurrentVersion() Version      { return Version{1, 0} }
func (v Version) Compatible() bool { return v.Major == 1 }
func (v Version) Negotiated() (Version, error) {
	if !v.Compatible() {
		return Version{}, ErrInvalid
	}
	return CurrentVersion(), nil
}

// Decimal is an unsigned canonical decimal string. Its bound depends on its field.
type Decimal string

func decimal(v Decimal, max uint64, positive bool) bool {
	s := string(v)
	if len(s) == 0 || len(s) > 20 || len(s) > 1 && s[0] == '0' {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	n, e := strconv.ParseUint(s, 10, 64)
	return e == nil && n <= max && (!positive || n > 0)
}
func stable(v string, max int) bool {
	if len(v) == 0 || len(v) > max || v[0] < 'a' || v[0] > 'z' {
		return false
	}
	for i := range len(v) {
		b := v[i]
		if !(b >= 'a' && b <= 'z' || i > 0 && (b >= '0' && b <= '9' || b == '_' || b == '.' || b == '-')) {
			return false
		}
	}
	return true
}
func text(v string, max int, empty bool) bool {
	if (!empty && v == "") || len(v) > max || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
