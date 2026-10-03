package foundation

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type objectID struct{}

func TestIDWireAndEntropy(t *testing.T) {
	const valid = "01900000-0000-7000-8000-000000000001"
	id, err := ParseID[objectID](valid)
	if err != nil || id.String() != valid {
		t.Fatalf("valid ID: %v", err)
	}
	encoded, err := json.Marshal(id)
	if err != nil || string(encoded) != `"`+valid+`"` {
		t.Fatalf("JSON: %s, %v", encoded, err)
	}
	var decoded ID[objectID]
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != id {
		t.Fatalf("round trip: %v", err)
	}
	for _, invalid := range []string{
		"", strings.ToUpper(valid[:35] + "a"), "00000000-0000-0000-0000-000000000000",
		"01900000-0000-4000-8000-000000000001", "01900000-0000-7000-c000-000000000001",
		"01900000-0000-7000-7000-000000000001", strings.ReplaceAll(valid, "-", ""),
		"{" + valid + "}", " " + valid, valid + " ", "urn:uuid:" + valid, valid[:35] + "g",
	} {
		if _, err := ParseID[objectID](invalid); err == nil {
			t.Errorf("accepted noncanonical ID %q", invalid)
		}
	}
	for _, invalid := range []string{`null`, `1`, `{}`, `[]`, `true`} {
		before := decoded
		if err := json.Unmarshal([]byte(invalid), &decoded); err == nil || decoded != before {
			t.Errorf("accepted or mutated on %s", invalid)
		}
	}
	if _, err := json.Marshal(ID[objectID]{}); err == nil {
		t.Fatal("zero ID serialized")
	}
	now := time.UnixMilli(0x010203040506)
	generated, err := newID[objectID](now, bytes.NewReader(make([]byte, 10)))
	if err != nil || generated.String() != "01020304-0506-7000-8000-000000000000" {
		t.Fatalf("UUID bit layout: %s %v", generated, err)
	}
	if _, err := newID[objectID](now, strings.NewReader("")); err == nil {
		t.Fatal("entropy failure fell back")
	}
	if _, err := newID[objectID](time.UnixMilli(-1), strings.NewReader("")); err == nil {
		t.Fatal("invalid timestamp accepted")
	}
}

func TestConcurrentIDGeneration(t *testing.T) {
	const workers, perWorker = 32, 128
	ids := make(chan ID[objectID], workers*perWorker)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range perWorker {
				id, err := NewID[objectID]()
				if err != nil {
					t.Error(err)
					return
				}
				ids <- id
			}
		})
	}
	wg.Wait()
	close(ids)
	seen := make(map[ID[objectID]]bool)
	for id := range ids {
		if id.Validate() != nil || seen[id] {
			t.Fatalf("invalid/duplicate ID: %s", id)
		}
		seen[id] = true
	}
	if len(seen) != workers*perWorker {
		t.Fatal("missing generated IDs")
	}
}

func TestInstantPrecisionAndOffset(t *testing.T) {
	for input, want := range map[string]string{
		"2026-10-03T12:34:56Z":             "2026-10-03T12:34:56.000000Z",
		"2026-10-03T12:34:56.1+02:30":      "2026-10-03T10:04:56.100000Z",
		"2024-02-29T23:59:59.123456-01:00": "2024-03-01T00:59:59.123456Z",
		"0000-01-01T00:00:00Z":             "0000-01-01T00:00:00.000000Z",
	} {
		i, err := ParseInstant(input)
		if err != nil || i.String() != want || i.Time().Location() != time.UTC {
			t.Fatalf("instant %s: %s %v", input, i, err)
		}
		encoded, err := json.Marshal(i)
		if err != nil || string(encoded) != `"`+want+`"` {
			t.Fatalf("wire instant: %s %v", encoded, err)
		}
		var decoded Instant
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.Time().Equal(i.Time()) {
			t.Fatalf("round trip: %v", err)
		}
	}
	clock := time.Date(2026, 10, 3, 12, 0, 0, 123456789, time.FixedZone("local", 3600))
	i, err := NewInstant(clock)
	if err != nil || i.String() != "2026-10-03T11:00:00.123456Z" {
		t.Fatalf("clock truncation: %s %v", i, err)
	}
	for _, input := range []string{
		"2026-10-03T12:34:56.1234560Z", "2026-02-29T12:00:00Z", "2026-02-30T12:00:00Z",
		"2026-10-03T24:00:00Z", "2026-10-03T12:60:00Z", "2026-10-03T12:00:60Z",
		"2026-10-03T12:00:00+24:00", "2026-10-03T12:00:00+00:60", "2026-10-03T12:00:00+1:00",
		"2026-10-03t12:00:00z", "2026-10-03T12:00:00,1Z", "2026-10-03T12:00:00.Z",
		"2026-10-03 12:00:00Z", "2026-10-03T12:00:00", "0000-01-01T00:00:00+01:00",
		"9999-12-31T23:59:59-01:00",
	} {
		if _, err := ParseInstant(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	if _, err := NewInstant(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("unsupported year")
	}
	for _, input := range []string{`null`, `0`, `false`, `{}`} {
		if err := json.Unmarshal([]byte(input), &i); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
}

type integerScalar interface {
	encoding.TextMarshaler
	encoding.TextUnmarshaler
	json.Marshaler
	json.Unmarshaler
	Validate() error
	String() string
}

func TestIntegerWireBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		minimum string
		create  func() integerScalar
	}{
		{"Version", "1", func() integerScalar { return new(Version) }},
		{"Revision", "1", func() integerScalar { return new(Revision) }},
		{"Sequence", "1", func() integerScalar { return new(Sequence) }},
		{"Progress", "0", func() integerScalar { return new(Progress) }},
		{"DurationMS", "0", func() integerScalar { return new(DurationMS) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, input := range []string{tc.minimum, "1", "9007199254740993", "9223372036854775807"} {
				n := tc.create()
				if err := n.UnmarshalText([]byte(input)); err != nil || n.String() != input || n.Validate() != nil {
					t.Fatalf("parse %s: %v", input, err)
				}
				b, err := json.Marshal(n)
				if err != nil || string(b) != `"`+input+`"` {
					t.Fatalf("wire %s: %s %v", input, b, err)
				}
				if err := json.Unmarshal(b, tc.create()); err != nil {
					t.Fatal(err)
				}
			}
			invalid := []string{"", "-1", "+1", "01", "00", " 1", "1 ", "1.0", "1e0", "9223372036854775808", "18446744073709551615", "１", "-0"}
			if tc.minimum == "1" {
				invalid = append(invalid, "0")
			}
			for _, input := range invalid {
				n := tc.create()
				_ = n.UnmarshalText([]byte("42"))
				if err := n.UnmarshalText([]byte(input)); err == nil || n.String() != "42" {
					t.Errorf("accepted/mutated text %q", input)
				}
				encoded, _ := json.Marshal(input)
				if err := json.Unmarshal(encoded, n); err == nil || n.String() != "42" {
					t.Errorf("accepted/mutated JSON %s", encoded)
				}
			}
			for _, input := range []string{`null`, `1`, `9007199254740993`, `true`, `{}`, `[]`} {
				if err := json.Unmarshal([]byte(input), tc.create()); err == nil {
					t.Errorf("accepted token %s", input)
				}
			}
		})
	}
	if _, err := json.Marshal(Version(0)); err == nil {
		t.Fatal("zero editable version")
	}
	if _, err := json.Marshal(DurationMS(-1)); err == nil {
		t.Fatal("negative duration")
	}
}

func TestDigestKeyAndPage(t *testing.T) {
	digest := "sha256:" + strings.Repeat("0123456789abcdef", 4)
	d, err := ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(d)
	var roundtrip Digest
	if err != nil || json.Unmarshal(encoded, &roundtrip) != nil || d != roundtrip {
		t.Fatal("digest round trip")
	}
	for _, input := range []string{"", digest[:70], digest + "a", strings.ToUpper(digest), "sha512:" + digest[7:], "sha256:" + strings.Repeat("g", 64)} {
		if _, err := ParseDigest(input); err == nil {
			t.Errorf("accepted digest %q", input)
		}
	}
	for _, input := range []string{"a", "Aa09._:/-", strings.Repeat("x", 128)} {
		key, err := ParseIdempotencyKey(input)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := key.MarshalText()
		var decoded IdempotencyKey
		if decoded.UnmarshalText(b) != nil || decoded != key {
			t.Fatal("key text round trip")
		}
	}
	for _, input := range []string{"", strings.Repeat("x", 129), "a b", "é", "a\nb", "a?b", "a=b", "a%b"} {
		if _, err := ParseIdempotencyKey(input); err == nil {
			t.Errorf("accepted key %q", input)
		}
	}
	for _, input := range []string{`null`, `1`, `true`} {
		if json.Unmarshal([]byte(input), &roundtrip) == nil {
			t.Fatal("invalid digest token")
		}
		var key IdempotencyKey
		if json.Unmarshal([]byte(input), &key) == nil {
			t.Fatal("invalid key token")
		}
	}
	for _, input := range []string{`{}`, `{"limit":1}`, `{"cursor":"opaque","limit":200}`} {
		var page PageRequest
		if err := json.Unmarshal([]byte(input), &page); err != nil || page.Validate() != nil {
			t.Fatalf("page %s: %v", input, err)
		}
		if input == `{}` && page.Limit != 50 {
			t.Fatal("default page limit")
		}
	}
	for _, input := range []string{`{"limit":0}`, `{"limit":201}`, `{"limit":-1}`, `{"limit":null}`, `{"limit":"50"}`, `{"limit":1.0}`, `null`} {
		var page PageRequest
		if err := json.Unmarshal([]byte(input), &page); err == nil {
			t.Errorf("accepted page %s", input)
		}
	}
	b, err := json.Marshal(Page[int]{})
	if err != nil || string(b) != `{"items":[]}` {
		t.Fatalf("empty page: %s %v", b, err)
	}
	b, err = json.Marshal(Page[int]{Items: []int{1, 2}, NextCursor: "next"})
	if err != nil || string(b) != `{"items":[1,2],"next_cursor":"next"}` {
		t.Fatalf("page envelope: %s %v", b, err)
	}
}

func TestCommandMetaAndSafeFault(t *testing.T) {
	id, _ := NewID[Request]()
	v := Version(1)
	m := CommandMeta{RequestID: id, IdempotencyKey: "intent", ExpectedVersion: &v}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	v = 0
	if m.Validate() == nil {
		t.Fatal("zero expected version")
	}
	m.ExpectedVersion = nil
	if m.Validate() != nil {
		t.Fatal("absent version should be explicit")
	}
	if (CommandMeta{}).Validate() == nil {
		t.Fatal("zero metadata")
	}
	const secret = "credential-SENTINEL-provider-body"
	cause := errors.New(secret)
	f := NewFault(DependencyUnavailable, Unknown).WithCause(cause)
	if !errors.Is(f, cause) {
		t.Fatal("lost cause identity")
	}
	if f.Error() != "DEPENDENCY_UNAVAILABLE" {
		t.Fatal("unsafe error identity")
	}
	for _, format := range []string{"%s", "%v", "%+v", "%#v", "%q"} {
		if output := fmt.Sprintf(format, f); strings.Contains(output, secret) {
			t.Fatalf("leak from %s", format)
		}
	}
	b, err := json.Marshal(struct {
		Failure *Fault `json:"failure"`
	}{f})
	if err != nil || bytes.Contains(b, []byte(secret)) {
		t.Fatal("cause leaked through JSON")
	}
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Error("test", "fault", f)
	if strings.Contains(log.String(), secret) {
		t.Fatal("cause leaked through logging")
	}
	unknown := NewFault(Code(secret), CommitState(secret)).WithCause(cause)
	if unknown.Error() != "INTERNAL_ERROR" || unknown.CommitState != Unknown {
		t.Fatal("unknown code/state not normalized")
	}
	for _, field := range []FieldError{{Path: "", Code: "INVALID"}, {Path: "/expected_version", Code: "STALE_VERSION"}, {Path: "/a~1b/~0c", Code: "INVALID"}} {
		if field.Validate() != nil {
			t.Fatalf("valid JSON pointer: %+v", field)
		}
	}
	for _, field := range []FieldError{{Path: "field", Code: "INVALID"}, {Path: "/~", Code: "INVALID"}, {Path: "/~2", Code: "INVALID"}, {Path: "/x", Code: "raw message"}} {
		if field.Validate() == nil {
			t.Fatalf("accepted invalid field error: %+v", field)
		}
	}
}

type rawCause string

func (e rawCause) Error() string { return string(e) }

func TestFaultCauseCannotLeakThroughEnclosingValues(t *testing.T) {
	const secret = "credential-SENTINEL-RAW-CAUSE"
	for _, cause := range []error{errors.New(secret), rawCause(secret)} {
		base := NewFault(DependencyUnavailable, Unknown)
		fault := base.WithCause(cause)
		if errors.Unwrap(base) != nil || errors.Unwrap(fault) != cause || !errors.Is(fault, cause) {
			t.Fatal("WithCause must preserve explicit cause identity without mutating the original")
		}
		wrapped := fmt.Errorf("safe context: %w", fault)
		var recovered *Fault
		if !errors.As(wrapped, &recovered) || recovered != fault || !errors.Is(wrapped, cause) {
			t.Fatal("fault error identity lost through wrapping")
		}
		for _, value := range []any{
			fault, *fault, struct{ Fault *Fault }{fault}, struct{ Err error }{fault},
			struct{ fault Fault }{*fault}, struct{ err error }{fault},
			[]Fault{*fault}, map[string]Fault{"fault": *fault},
		} {
			for _, format := range []string{"%s", "%q", "%v", "%+v", "%#v", "%x", "%d"} {
				if strings.Contains(fmt.Sprintf(format, value), secret) {
					t.Errorf("raw cause leaked through %T formatted with %s", value, format)
				}
			}
			b, err := json.Marshal(value)
			if err != nil || bytes.Contains(b, []byte(secret)) {
				t.Errorf("JSON leak or encoding error for %T: %v", value, err)
			}
			for _, textLog := range []bool{false, true} {
				var logs bytes.Buffer
				var handler slog.Handler = slog.NewJSONHandler(&logs, nil)
				if textLog {
					handler = slog.NewTextHandler(&logs, nil)
				}
				slog.New(handler).Error("test", "fault", value)
				if strings.Contains(logs.String(), secret) {
					t.Errorf("raw cause leaked through %T logged as text=%v", value, textLog)
				}
			}
		}
	}
}
