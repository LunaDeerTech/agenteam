package foundation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

type PageRequest struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit"`
}

func DefaultPageRequest() PageRequest { return PageRequest{Limit: DefaultPageLimit} }

func (p PageRequest) Validate() error {
	if p.Limit < 1 || p.Limit > MaxPageLimit {
		return errors.New("invalid page limit")
	}
	return nil
}

// UnmarshalJSON defaults only an omitted limit. Explicit zero and null are invalid.
// HTTP decoding additionally enforces duplicate keys and exact DTO field names.
func (p *PageRequest) UnmarshalJSON(b []byte) error {
	type wire struct {
		Cursor string          `json:"cursor,omitempty"`
		Limit  json.RawMessage `json:"limit"`
	}
	var v wire
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if len(bytes.TrimSpace(b)) == 0 || bytes.TrimSpace(b)[0] != '{' {
		return errScalar
	}
	if err := dec.Decode(&v); err != nil {
		return errScalar
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return errScalar
	}
	next := DefaultPageRequest()
	next.Cursor = v.Cursor
	if v.Limit != nil {
		if bytes.Equal(bytes.TrimSpace(v.Limit), []byte("null")) {
			return errScalar
		}
		if err := json.Unmarshal(v.Limit, &next.Limit); err != nil {
			return errScalar
		}
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*p = next
	return nil
}

func (p PageRequest) MarshalJSON() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	type wire PageRequest
	return json.Marshal(wire(p))
}

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func (p Page[T]) MarshalJSON() ([]byte, error) {
	type wire Page[T]
	if p.Items == nil {
		p.Items = []T{}
	}
	return json.Marshal(wire(p))
}
