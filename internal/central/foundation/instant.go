package foundation

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

const instantLayout = "2006-01-02T15:04:05.000000Z"

var (
	errInstant     = errors.New("invalid microsecond instant")
	instantPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)
)

// Instant is a UTC timestamp at microsecond precision.
type Instant struct{ value time.Time }

func NewInstant(t time.Time) (Instant, error) {
	t = t.UTC().Truncate(time.Microsecond)
	if t.Year() < 0 || t.Year() > 9999 {
		return Instant{}, errInstant
	}
	return Instant{value: t}, nil
}

func ParseInstant(s string) (Instant, error) {
	if !instantPattern.MatchString(s) {
		return Instant{}, errInstant
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return Instant{}, errInstant
	}
	return NewInstant(t)
}

func (i Instant) Validate() error {
	if i.value.Year() < 0 || i.value.Year() > 9999 || i.value.Nanosecond()%1000 != 0 {
		return errInstant
	}
	return nil
}

func (i Instant) Time() time.Time { return i.value.UTC() }
func (i Instant) String() string  { return i.Time().Format(instantLayout) }

func (i Instant) MarshalText() ([]byte, error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	return []byte(i.String()), nil
}

func (i *Instant) UnmarshalText(b []byte) error {
	value, err := ParseInstant(string(b))
	if err == nil {
		*i = value
	}
	return err
}

func (i Instant) MarshalJSON() ([]byte, error) {
	b, err := i.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(b))
}

func (i *Instant) UnmarshalJSON(b []byte) error {
	s, err := scalarString(b)
	if err != nil {
		return errInstant
	}
	return i.UnmarshalText([]byte(s))
}
