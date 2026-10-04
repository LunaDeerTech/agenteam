package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type testPayload struct {
	Change string   `json:"change"`
	IDs    []string `json:"ids"`
}

func fixtureType(t *testing.T) (*Catalog, EventType[testPayload], Header) {
	t.Helper()
	c := NewCatalog()
	et, err := DefineEvent(c, Definition[testPayload]{Schema: Schema{"fixture", "fixture.changed", "fixture", 1}, Codec: JSONCodec[testPayload]{}, Validate: func(v testPayload) error {
		if v.Change == "" || len(v.IDs) == 0 {
			return invalid()
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := foundation.NewID[EventIdentity]()
	agg, _ := foundation.NewID[Aggregate]()
	at, _ := foundation.NewInstant(time.Now())
	v := foundation.Version(1)
	return c, et, Header{EventID: id, EventType: "fixture.changed", SchemaVersion: 1, OccurredAt: at, Scope: Scope{Kind: SystemScope}, AggregateType: "fixture", AggregateID: agg, AggregateVersion: &v}
}

func TestTypedEventImmutableAndSafe(t *testing.T) {
	c, et, h := fixtureType(t)
	original := testPayload{"private-change", []string{"private-id"}}
	e, err := NewEvent(et, h, original)
	if err != nil {
		t.Fatal(err)
	}
	original.IDs[0] = "mutated"
	*h.AggregateVersion = 9
	if e.Header().AggregateVersion == nil || *e.Header().AggregateVersion != 1 || !c.Owns(e) || e.Summary().PayloadDigest.Validate() != nil {
		t.Fatal("header ownership/copy")
	}
	b := e.PayloadBytes()
	b[0] = '!'
	got, err := DecodeEvent(et, e)
	if err != nil || got.IDs[0] != "private-id" {
		t.Fatal("mutable payload", err)
	}
	got.IDs[0] = "changed"
	for _, v := range []any{e, &e, struct{ event Event }{e}, []Event{e}, struct{ event any }{e}} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			s := fmt.Sprintf(verb, v)
			if strings.Contains(s, "private-") {
				t.Fatal("fmt leaked payload")
			}
		}
		var out bytes.Buffer
		slog.New(slog.NewTextHandler(&out, nil)).Info("safe", "value", v)
		if strings.Contains(out.String(), "private-") {
			t.Fatal("slog leaked")
		}
	}
	data, err := json.Marshal(e)
	if err != nil || string(data) != `"domain_event"` {
		t.Fatal("unsafe JSON")
	}
	if json.Unmarshal([]byte(`{}`), &e) == nil {
		t.Fatal("JSON created capability")
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 100 {
				v, err := DecodeEvent(et, e)
				if err != nil || v.IDs[0] != "private-id" {
					t.Error("concurrent mutation")
				}
				v.IDs[0] = "other"
			}
		})
	}
	wg.Wait()
}

func TestCodecStrictBoundariesAndCatalogIdentity(t *testing.T) {
	c, et, h := fixtureType(t)
	for _, raw := range []string{`{"change":"a","ids":["x"],"extra":1}`, `{"change":"a","change":"b","ids":["x"]}`, `{"change":"a","ids":["x"]} {}`, `{"change":"a","ids":[]}`, `{"ids":["x"]}`, `{"change":"a","ids":["x"],"Change":"b"}`} {
		if _, err := c.Restore("fixture", h, []byte(raw)); err == nil {
			t.Fatal("invalid payload accepted", raw)
		}
	}
	if _, err := NewEvent(et, h, testPayload{strings.Repeat("x", MaxPayloadBytes), []string{"x"}}); err == nil {
		t.Fatal("oversize")
	}
	e, err := NewEvent(et, h, testPayload{"a", []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	other, otherType, _ := fixtureType(t)
	if other.Owns(e) {
		t.Fatal("catalog issuer confused")
	}
	if _, err = DecodeEvent(otherType, e); err == nil {
		t.Fatal("cross-catalog decode")
	}
	h.SchemaVersion = 2
	if _, err = c.Restore("fixture", h, e.PayloadBytes()); err == nil {
		t.Fatal("unknown schema")
	}
	if err = c.Seal(); err != nil {
		t.Fatal(err)
	}
	if _, err = DefineEvent(c, Definition[testPayload]{Schema: Schema{"fixture", "other.event", "fixture", 1}, Codec: JSONCodec[testPayload]{}, Validate: func(testPayload) error { return nil }}); err == nil {
		t.Fatal("sealed catalog changed")
	}
	if _, err = DefineEvent(NewCatalog(), Definition[map[string]any]{Schema: Schema{"fixture", "bad.event", "fixture", 1}, Codec: JSONCodec[map[string]any]{}, Validate: func(map[string]any) error { return nil }}); err == nil {
		t.Fatal("untyped schema")
	}
}

func TestHeaderSequenceCanonicalPositiveDecimal(t *testing.T) {
	_, _, h := fixtureType(t)
	h.AggregateVersion = nil
	for _, v := range []foundation.Sequence{1, 9007199254740993, foundation.Sequence(math.MaxInt64)} {
		h.AggregateSequence = &v
		raw, err := json.Marshal(h)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(raw, []byte(`"aggregate_sequence":"`+v.String()+`"`)) {
			t.Fatal("sequence is not a decimal string")
		}
		got, err := DecodeHeader(raw)
		if err != nil || *got.AggregateSequence != v {
			t.Fatal("sequence roundtrip", err)
		}
		for _, bad := range []string{`0`, `1`, `"0"`, `"01"`, `"-1"`, `"9223372036854775808"`} {
			r := bytes.Replace(raw, []byte(`"aggregate_sequence":"`+v.String()+`"`), []byte(`"aggregate_sequence":`+bad), 1)
			if _, err = DecodeHeader(r); err == nil {
				t.Fatal("bad sequence accepted", bad)
			}
		}
	}
	h.AggregateSequence = nil
	if h.Validate() == nil {
		t.Fatal("both versions absent")
	}
}

type numericPayload struct {
	Value float64 `json:"value"`
}
type panicCodec struct{}

func (panicCodec) Encode(testPayload) ([]byte, error) { panic("private codec error") }
func (panicCodec) Decode([]byte) (testPayload, error) { panic("private codec error") }

func TestCodecPanicNonFiniteAndMalformedJSONAreRejected(t *testing.T) {
	_, _, h := fixtureType(t)
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		c := NewCatalog()
		et, err := DefineEvent(c, Definition[numericPayload]{Schema: Schema{"fixture", "fixture.changed", "fixture", 1}, Codec: JSONCodec[numericPayload]{}, Validate: func(numericPayload) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = NewEvent(et, h, numericPayload{value}); err == nil {
			t.Fatal("nonfinite payload")
		}
	}
	et, err := DefineEvent(NewCatalog(), Definition[testPayload]{Schema: Schema{"fixture", "fixture.changed", "fixture", 1}, Codec: panicCodec{}, Validate: func(testPayload) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewEvent(et, h, testPayload{}); err == nil || strings.Contains(fmt.Sprint(err), "private") {
		t.Fatal("panic unsafe")
	}
	for _, raw := range [][]byte{[]byte(`{"value":1e10000}`), []byte(`{"value":{"x":1,"x":2}}`), []byte("{\"value\":\"\xff\"}"), []byte(strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66))} {
		if _, err = canonicalJSON(raw); err == nil {
			t.Fatal("malformed JSON accepted")
		}
	}
	raw, _ := json.Marshal(h)
	raw = bytes.Replace(raw, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":2`), 1)
	if _, err = DecodeHeader(raw); err == nil {
		t.Fatal("duplicate header accepted")
	}
}

type failingDecodeCodec struct{ fail bool }

func (c *failingDecodeCodec) Encode(v testPayload) ([]byte, error) { return json.Marshal(v) }
func (c *failingDecodeCodec) Decode(raw []byte) (testPayload, error) {
	if c.fail {
		return testPayload{Change: "private-partial"}, fmt.Errorf("private codec payload: %s", raw)
	}
	var v testPayload
	err := json.Unmarshal(raw, &v)
	return v, err
}
func TestDecodeFailureHasOnlySafeProjection(t *testing.T) {
	_, _, h := fixtureType(t)
	codec := &failingDecodeCodec{}
	et, err := DefineEvent(NewCatalog(), Definition[testPayload]{Schema: Schema{"fixture", "fixture.changed", "fixture", 1}, Codec: codec, Validate: func(testPayload) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewEvent(et, h, testPayload{"private-payload", []string{"private-id"}})
	if err != nil {
		t.Fatal(err)
	}
	codec.fail = true
	value, err := DecodeEvent(et, e)
	var fault *foundation.Fault
	if !errors.As(err, &fault) || value.Change != "" || value.IDs != nil {
		t.Fatal("unsafe partial decode result")
	}
	for _, v := range []any{err, struct{ err error }{err}, []error{err}} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(verb, v), "private-") {
				t.Fatal("codec error leaked")
			}
		}
		var output bytes.Buffer
		slog.New(slog.NewTextHandler(&output, nil)).Info("decode", "error", v)
		if strings.Contains(output.String(), "private-") {
			t.Fatal("codec error leaked in log")
		}
	}
}
func TestHeaderRequiresExactNamesAndPresence(t *testing.T) {
	_, _, h := fixtureType(t)
	// Required presence must not exclude foundation's valid earliest Instant.
	h.OccurredAt, _ = foundation.NewInstant(time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC))
	raw, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeHeader(raw); err != nil {
		t.Fatal("valid year 0001", err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"event_id", "event_type", "schema_version", "occurred_at", "scope", "aggregate_type", "aggregate_id"} {
		saved := fields[required]
		delete(fields, required)
		missing, _ := json.Marshal(fields)
		if _, err = DecodeHeader(missing); err == nil {
			t.Fatal("missing field", required)
		}
		fields[strings.ToUpper(required)] = saved
		alias, _ := json.Marshal(fields)
		if _, err = DecodeHeader(alias); err == nil {
			t.Fatal("alias field", required)
		}
		fields[required] = saved
		alias, _ = json.Marshal(fields)
		if _, err = DecodeHeader(alias); err == nil {
			t.Fatal("duplicate alias", required)
		}
		delete(fields, strings.ToUpper(required))
	}
	for _, scope := range []string{`{"KIND":"system"}`, `{"kind":"system","KIND":"system"}`, `{"kind":"system","PROJECT_ID":""}`, `{"kind":null}`} {
		var value Scope
		if json.Unmarshal([]byte(scope), &value) == nil {
			t.Fatal("scope alias/null accepted")
		}
	}
}
