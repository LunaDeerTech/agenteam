package foundation

import "strconv"

// Version, Revision and Sequence start at one. Progress and DurationMS allow
// zero. Across JSON all five types are canonical decimal strings, never floats.

type Version int64

func ParseVersion(s string) (Version, error) {
	n, err := parseInteger(s, 1)
	return Version(n), err
}
func (n Version) Validate() error {
	if n < 1 {
		return errScalar
	}
	return nil
}
func (n Version) String() string               { return strconv.FormatInt(int64(n), 10) }
func (n Version) MarshalText() ([]byte, error) { return integerText(int64(n), 1) }
func (n *Version) UnmarshalText(b []byte) error {
	value, err := ParseVersion(string(b))
	if err == nil {
		*n = value
	}
	return err
}
func (n Version) MarshalJSON() ([]byte, error) { return integerJSON(int64(n), 1) }
func (n *Version) UnmarshalJSON(b []byte) error {
	s, err := scalarString(b)
	if err != nil {
		return err
	}
	return n.UnmarshalText([]byte(s))
}

type Revision int64

func ParseRevision(s string) (Revision, error) {
	n, err := parseInteger(s, 1)
	return Revision(n), err
}
func (n Revision) Validate() error {
	if n < 1 {
		return errScalar
	}
	return nil
}
func (n Revision) String() string               { return strconv.FormatInt(int64(n), 10) }
func (n Revision) MarshalText() ([]byte, error) { return integerText(int64(n), 1) }
func (n *Revision) UnmarshalText(b []byte) error {
	value, err := ParseRevision(string(b))
	if err == nil {
		*n = value
	}
	return err
}
func (n Revision) MarshalJSON() ([]byte, error) { return integerJSON(int64(n), 1) }
func (n *Revision) UnmarshalJSON(b []byte) error {
	s, err := scalarString(b)
	if err != nil {
		return err
	}
	return n.UnmarshalText([]byte(s))
}

type Sequence int64

func ParseSequence(s string) (Sequence, error) {
	n, err := parseInteger(s, 1)
	return Sequence(n), err
}
func (n Sequence) Validate() error {
	if n < 1 {
		return errScalar
	}
	return nil
}
func (n Sequence) String() string               { return strconv.FormatInt(int64(n), 10) }
func (n Sequence) MarshalText() ([]byte, error) { return integerText(int64(n), 1) }
func (n *Sequence) UnmarshalText(b []byte) error {
	value, err := ParseSequence(string(b))
	if err == nil {
		*n = value
	}
	return err
}
func (n Sequence) MarshalJSON() ([]byte, error) { return integerJSON(int64(n), 1) }
func (n *Sequence) UnmarshalJSON(b []byte) error {
	s, err := scalarString(b)
	if err != nil {
		return err
	}
	return n.UnmarshalText([]byte(s))
}

type Progress int64

func ParseProgress(s string) (Progress, error) {
	n, err := parseInteger(s, 0)
	return Progress(n), err
}
func (n Progress) Validate() error {
	if n < 0 {
		return errScalar
	}
	return nil
}
func (n Progress) String() string               { return strconv.FormatInt(int64(n), 10) }
func (n Progress) MarshalText() ([]byte, error) { return integerText(int64(n), 0) }
func (n *Progress) UnmarshalText(b []byte) error {
	value, err := ParseProgress(string(b))
	if err == nil {
		*n = value
	}
	return err
}
func (n Progress) MarshalJSON() ([]byte, error) { return integerJSON(int64(n), 0) }
func (n *Progress) UnmarshalJSON(b []byte) error {
	s, err := scalarString(b)
	if err != nil {
		return err
	}
	return n.UnmarshalText([]byte(s))
}

type DurationMS int64

func ParseDurationMS(s string) (DurationMS, error) {
	n, err := parseInteger(s, 0)
	return DurationMS(n), err
}
func (n DurationMS) Validate() error {
	if n < 0 {
		return errScalar
	}
	return nil
}
func (n DurationMS) String() string               { return strconv.FormatInt(int64(n), 10) }
func (n DurationMS) MarshalText() ([]byte, error) { return integerText(int64(n), 0) }
func (n *DurationMS) UnmarshalText(b []byte) error {
	value, err := ParseDurationMS(string(b))
	if err == nil {
		*n = value
	}
	return err
}
func (n DurationMS) MarshalJSON() ([]byte, error) { return integerJSON(int64(n), 0) }
func (n *DurationMS) UnmarshalJSON(b []byte) error {
	s, err := scalarString(b)
	if err != nil {
		return err
	}
	return n.UnmarshalText([]byte(s))
}
