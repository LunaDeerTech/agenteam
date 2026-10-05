package contract

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func lookupRequest(t *testing.T, kind MutationKind) WriteCommandLookupRequest {
	t.Helper()
	a, _ := id.NewHuman(contractID[id.User](t), contractID[id.Session](t))
	c, _ := f.NewCommandIdentity("secret", []string{a.Details().UserID}, string(kind), "private-lookup-key-sentinel")
	r := WriteCommandLookupRequest{Actor: a, Scope: id.SystemScope(), Identity: c, Kind: kind, Purpose: Model}
	if kind != Create {
		r.Ref, _ = NewCredentialRef(contractID[Credential](t), r.Scope)
		r.ExpectedVersion = 7
	}
	return r
}

func TestWriteCommandLookupIdentityAndScope(t *testing.T) {
	for _, kind := range []MutationKind{Create, Update, Delete} {
		r := lookupRequest(t, kind)
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		changes := map[string]func(*WriteCommandLookupRequest){
			"actor":   func(r *WriteCommandLookupRequest) { r.Actor = id.Actor{} },
			"project": func(r *WriteCommandLookupRequest) { r.Scope, _ = id.InProject(contractID[id.Project](t)) },
			"purpose": func(r *WriteCommandLookupRequest) { r.Purpose = SMTP },
			"kind":    func(r *WriteCommandLookupRequest) { r.Kind = "rename" },
			"namespace": func(r *WriteCommandLookupRequest) {
				r.Identity, _ = f.NewCommandIdentity("secret.account", []string{r.Actor.Details().UserID}, string(r.Kind), "key")
			},
			"owner": func(r *WriteCommandLookupRequest) {
				r.Identity, _ = f.NewCommandIdentity("secret", []string{contractID[id.User](t).String()}, string(r.Kind), "key")
			},
			"owners": func(r *WriteCommandLookupRequest) {
				r.Identity, _ = f.NewCommandIdentity("secret", []string{r.Actor.Details().UserID, contractID[id.User](t).String()}, string(r.Kind), "key")
			},
			"command": func(r *WriteCommandLookupRequest) {
				r.Identity, _ = f.NewCommandIdentity("secret", []string{r.Actor.Details().UserID}, "wrong", "key")
			},
		}
		for name, change := range changes {
			t.Run(string(kind)+"/"+name, func(t *testing.T) {
				copy := r
				change(&copy)
				if copy.Validate() == nil {
					t.Fatal("invalid request accepted")
				}
			})
		}
		if kind == Create {
			r.ExpectedVersion = 1
			if r.Validate() == nil {
				t.Fatal("create version")
			}
			r.ExpectedVersion = 0
			r.Ref, _ = NewCredentialRef(contractID[Credential](t), id.SystemScope())
			if r.Validate() == nil {
				t.Fatal("create ref")
			}
		} else {
			r.ExpectedVersion = f.Version(math.MaxInt64)
			if r.Validate() == nil {
				t.Fatal("overflow successor")
			}
			r.ExpectedVersion = 0
			if r.Validate() == nil {
				t.Fatal("missing version")
			}
		}
	}
}

func TestWriteCommandLookupSafeProjection(t *testing.T) {
	r := lookupRequest(t, Update)
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, r), "private-lookup-key-sentinel") {
			t.Fatal("key leaked")
		}
	}
	b, err := json.Marshal(r)
	if err != nil || strings.Contains(string(b), "private-lookup-key-sentinel") {
		t.Fatal("JSON leaked", err)
	}
	b, err = json.Marshal(WriteCommandObservation{})
	if err != nil || string(b) != `{"observed":false,"result":null}` {
		t.Fatalf("absence=%s %v", b, err)
	}
}
