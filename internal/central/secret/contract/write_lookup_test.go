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

func TestProjectWriteCommandLookupIdentityScopeAndBounds(t *testing.T) {
	for _, kind := range []MutationKind{Create, Update, Delete} {
		t.Run(string(kind), func(t *testing.T) {
			r := lookupRequest(t, kind)
			project := contractID[id.Project](t)
			r.Scope, _ = id.InProject(project)
			r.Identity, _ = f.NewCommandIdentity("secret", []string{project.String(), r.Actor.Details().UserID}, string(kind), "original-key")
			if kind != Create {
				r.Ref, _ = NewCredentialRef(contractID[Credential](t), r.Scope)
				r.ExpectedVersion = f.Version(math.MaxInt64 - 1)
			}
			if err := r.Validate(); err != nil {
				t.Fatal("valid Project lookup rejected", err)
			}
			for name, owners := range map[string][]string{
				"missing-project": {r.Actor.Details().UserID},
				"reverse":         {r.Actor.Details().UserID, project.String()},
				"other-project":   {contractID[id.Project](t).String(), r.Actor.Details().UserID},
				"other-user":      {project.String(), contractID[id.User](t).String()},
				"extra":           {project.String(), r.Actor.Details().UserID, contractID[id.Session](t).String()},
			} {
				t.Run(name, func(t *testing.T) {
					bad := r
					bad.Identity, _ = f.NewCommandIdentity("secret", owners, string(kind), "original-key")
					if bad.Validate() == nil {
						t.Fatal("misbound Project identity accepted")
					}
				})
			}
			changes := map[string]func(*WriteCommandLookupRequest){
				"scope":   func(v *WriteCommandLookupRequest) { v.Scope, _ = id.InProject(contractID[id.Project](t)) },
				"system":  func(v *WriteCommandLookupRequest) { v.Scope = id.SystemScope() },
				"purpose": func(v *WriteCommandLookupRequest) { v.Purpose = MCP },
				"command": func(v *WriteCommandLookupRequest) {
					v.Identity, _ = f.NewCommandIdentity("secret", []string{project.String(), v.Actor.Details().UserID}, "unknown", "original-key")
				},
			}
			if kind == Create {
				changes["ref"] = func(v *WriteCommandLookupRequest) { v.Ref, _ = NewCredentialRef(contractID[Credential](t), v.Scope) }
				changes["expected"] = func(v *WriteCommandLookupRequest) { v.ExpectedVersion = 1 }
			} else {
				changes["zero-version"] = func(v *WriteCommandLookupRequest) { v.ExpectedVersion = 0 }
				changes["overflow"] = func(v *WriteCommandLookupRequest) { v.ExpectedVersion = f.Version(math.MaxInt64) }
				changes["ref-scope"] = func(v *WriteCommandLookupRequest) { v.Ref, _ = NewCredentialRef(v.Ref.Details().ID, id.SystemScope()) }
			}
			for name, change := range changes {
				t.Run(name, func(t *testing.T) {
					bad := r
					change(&bad)
					if bad.Validate() == nil {
						t.Fatal("invalid Project lookup accepted")
					}
				})
			}
		})
	}
}
