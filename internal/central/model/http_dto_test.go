package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const httpValidProvider = `{"name":"test","protocol":"openai-chat-completions","base_url":"https://provider.example/v1","enabled":false,"credential_ref":null,"options":{}}`
const httpValidModel = `{"name":"test","provider_model_id":"model","type":"chat","enabled":false,"parameters":{},"request_overwrite":{},"header_overwrite":{},"capabilities":{"tool_calls":false,"parallel_tool_calls":false,"streaming":true,"reasoning":false,"input_modalities":["text"],"output_modalities":["text"],"reasoning_efforts":[],"structured_output_modes":[],"context_length":"9007199254740993","max_output":null}}`

func decodeHTTP(t *testing.T, raw string, dst any) error {
	t.Helper()
	r := httptest.NewRequest("POST", "https://example.test", strings.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	return httpapi.DecodeJSON(httptest.NewRecorder(), r, dst, 0)
}

func TestSystemHTTPDTORequiredNullAndExactScalars(t *testing.T) {
	var p httpProviderInput
	if err := decodeHTTP(t, httpValidProvider, &p); err != nil {
		t.Fatal(err)
	}
	if v, err := p.input(); err != nil || v.Enabled || v.CredentialRef != nil {
		t.Fatal("explicit false/null", err)
	}
	var model httpModelInput
	if err := decodeHTTP(t, httpValidModel, &model); err != nil {
		t.Fatal(err)
	}
	v, err := model.input()
	if err != nil || v.Capabilities.ContextLength == nil || *v.Capabilities.ContextLength != 9007199254740993 {
		t.Fatal("integer rounded", err)
	}
	for _, tc := range []struct{ base, old, new string }{{httpValidProvider, `"enabled":false,`, ``}, {httpValidProvider, `"credential_ref":null,`, ``}, {httpValidProvider, `"options":{}`, `"options":null`}, {httpValidProvider, `"enabled":false`, `"enabled":null`}, {httpValidProvider, `"name":"test"`, `"name":"test","name":"again"`}, {httpValidProvider, `"options":{}`, `"options":{"x":1,"x":2}`}, {httpValidProvider, `"options":{}`, `"options":{},"actor":"fake"`}, {httpValidModel, `"max_output":null`, `"max_output":0`}, {httpValidModel, `"max_output":null`, `"max_output":"0"`}, {httpValidModel, `"context_length":"9007199254740993"`, `"context_length":9007199254740993`}, {httpValidModel, `"context_length":"9007199254740993"`, `"context_length":"092"`}, {httpValidModel, `"context_length":"9007199254740993"`, `"context_length":"9223372036854775808"`}, {httpValidModel, `"reasoning":false,`, ``}, {httpValidModel, `"reasoning_efforts":[]`, `"reasoning_efforts":null`}, {httpValidModel, `"max_output":null`, `"max_output":null,"extra":true`}, {httpValidModel, `"header_overwrite":{}`, `"header_overwrite":{"x":2}`}, {httpValidModel, `"parameters":{}`, `"parameters":null`}} {
		raw := strings.Replace(tc.base, tc.old, tc.new, 1)
		if tc.base == httpValidProvider {
			var d httpProviderInput
			err = decodeHTTP(t, raw, &d)
			if err == nil {
				_, err = d.input()
			}
		} else {
			var d httpModelInput
			err = decodeHTTP(t, raw, &d)
			if err == nil {
				_, err = d.input()
			}
		}
		if err == nil {
			t.Fatalf("invalid wire accepted: %s", tc.old)
		}
	}
	for _, raw := range []string{`"01"`, `1`, `null`, `"-1"`, `"9223372036854775808"`} {
		var d struct {
			Version f.Version `json:"version"`
		}
		if decodeHTTP(t, `{"version":`+raw+`}`, &d) == nil {
			t.Fatal("invalid version", raw)
		}
	}
	for _, raw := range []json.RawMessage{nil, []byte(`"bad"`), []byte(`0`)} {
		if _, err := httpNullableID[mc.Model](raw); err == nil {
			t.Fatal("invalid nullable ID")
		}
	}
}

func TestSystemHTTPListQueryCanonicalAndProjection(t *testing.T) {
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=01", "?limit=+1", "?limit=1&limit=2", "?cursor=", "?unknown=x", "?&", "?limit=1&", "?"} {
		r := httptest.NewRequest("GET", "https://example.test/"+query, nil)
		if _, _, err := httpListQuery(r, false); err == nil {
			t.Fatal("bad query", query)
		}
	}
	r := httptest.NewRequest("GET", "https://example.test/", nil)
	q, _, err := httpListQuery(r, false)
	if err != nil || q.Limit != 50 {
		t.Fatal("default", err)
	}
	if _, _, err = httpListQuery(r, true); err == nil {
		t.Fatal("missing provider")
	}
	var dto httpModelInput
	_ = decodeHTTP(t, httpValidModel, &dto)
	m, _ := dto.input()
	now, _ := f.NewInstant(time.Now())
	b, err := json.Marshal(httpModelDTO(mc.ModelView{ID: mustID[mc.Model](t), ProviderID: mustID[mc.Provider](t), Input: m, Version: 9007199254740993, CreatedAt: now, UpdatedAt: now}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"scope"`) || !bytes.Contains(b, []byte(`"version":"9007199254740993"`)) || !bytes.Contains(b, []byte(`"max_output":null`)) || !bytes.Contains(b, []byte(`"reasoning_efforts":[]`)) {
		t.Fatal("unsafe/inexact projection", string(b))
	}
}

type httpWritesSpy struct {
	sc.HumanWriteCommands
	lookup   func(sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error)
	metadata func(sc.CredentialRef) (sc.Metadata, error)
	execute  func(sc.WriteRequest) (sc.MutationResult, error)
}

func (s *httpWritesSpy) LookupWriteCommand(_ context.Context, r sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
	return s.lookup(r)
}
func (s *httpWritesSpy) Metadata(_ context.Context, _ id.Actor, r sc.CredentialRef) (sc.Metadata, error) {
	return s.metadata(r)
}
func (s *httpWritesSpy) ExecuteWrite(_ context.Context, r sc.WriteRequest) (sc.MutationResult, error) {
	return s.execute(r)
}

func TestSystemHTTPCredentialMaterialAndBoundedReplay(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		var captured sc.SecretMaterial
		h := &systemHTTP{writes: &httpWritesSpy{execute: func(r sc.WriteRequest) (sc.MutationResult, error) {
			captured = r.Value
			if r.Scope.Details().Kind != id.System || r.Purpose != sc.Model || r.Identity.OwnerIDs()[0] != r.Actor.Details().UserID {
				t.Fatal("untrusted identity")
			}
			if unknown {
				return sc.MutationResult{}, f.NewFault(f.CommitUnknown, f.Unknown)
			}
			return sc.MutationResult{}, nil
		}}}
		r := httptest.NewRequest("POST", "https://example.test", strings.NewReader(`{"value":"input-secret-sentinel"}`))
		r.Header.Set("Content-Type", "application/json")
		_, _ = h.createCredential(httptest.NewRecorder(), r, mc.CommandMeta{Actor: testActor(t), Scope: id.SystemScope(), Key: "key"})
		if captured.Use(func([]byte) error { return nil }) == nil {
			t.Fatal("material outlived HTTP call")
		}
	}
	var scalar httpCredentialValue
	_ = json.Unmarshal([]byte(`"input-secret-sentinel"`), &scalar)
	b, _ := json.Marshal(scalar)
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("input", "value", scalar)
	if strings.Contains(string(b)+fmt.Sprintf("%+v %#v", scalar, scalar)+log.String(), "input-secret-sentinel") {
		t.Fatal("material projected")
	}
	for _, mode := range []string{"observed", "metadata", "late-receipt", "denied", "lookup-error"} {
		t.Run(mode, func(t *testing.T) {
			actor := testActor(t)
			ref, _ := sc.NewCredentialRef(mustID[sc.Credential](t), id.SystemScope())
			request, _ := httpCredentialRequest(mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "key"}, sc.Update, ref, 3)
			lookups, metadata, writes := 0, 0, 0
			spy := &httpWritesSpy{lookup: func(r sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
				lookups++
				if mode == "lookup-error" {
					return sc.WriteCommandObservation{}, fault(f.DependencyUnavailable)
				}
				return sc.WriteCommandObservation{Observed: mode == "observed" || mode == "late-receipt" && lookups == 2}, nil
			}, metadata: func(sc.CredentialRef) (sc.Metadata, error) {
				metadata++
				if mode != "metadata" {
					return sc.Metadata{}, fault(f.NotFound)
				}
				return sc.Metadata{CredentialRef: ref, Purpose: sc.Model, Version: 3}, nil
			}, execute: func(r sc.WriteRequest) (sc.MutationResult, error) {
				writes++
				if r.ExpectedVersion != 3 {
					t.Fatal("TOCTOU expected changed")
				}
				return sc.MutationResult{}, f.NewFault(f.CommitUnknown, f.Unknown)
			}}
			_, err := (&systemHTTP{writes: spy}).executeCredential(context.Background(), request)
			want := mode != "denied" && mode != "lookup-error"
			if writes != map[bool]int{true: 1, false: 0}[want] || lookups > 2 || metadata > 1 || err == nil {
				t.Fatalf("unbounded/bypassed result l=%d m=%d w=%d e=%v", lookups, metadata, writes, err)
			}
		})
	}
}
