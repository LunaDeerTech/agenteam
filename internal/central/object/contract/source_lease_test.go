package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestSourceLeaseIssuerAndSourceOperationAreDistinct(t *testing.T) {
	id, _ := foundation.NewID[Lease]()
	issuer, other := NewSourceIssuer(), NewSourceIssuer()
	lease, e := NewSourceLease(issuer, id)
	if e != nil {
		t.Fatal(e)
	}
	if !lease.IssuedBy(issuer) || lease.IssuedBy(other) || lease.IssuedBy(SourceIssuer{}) || (SourceLease{}).IssuedBy(issuer) || lease.ID() != id {
		t.Fatal("source issuer binding")
	}
	if _, e = NewSourceLease(SourceIssuer{}, id); e == nil {
		t.Fatal("zero issuer")
	}
	for _, v := range []any{lease, issuer, struct {
		lease  SourceLease
		issuer SourceIssuer
	}{lease, issuer}} {
		for _, f := range []string{"%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(f, v), id.String()) {
				t.Fatal("private source state disclosed")
			}
		}
	}
	if json.Unmarshal([]byte(`{}`), &lease) == nil || json.Unmarshal([]byte(`{}`), &issuer) == nil {
		t.Fatal("JSON source capability")
	}
	actor, owner, object := accessValues(t)
	project, _ := foundation.ParseID[identity.Project](testID)
	meta := ObjectMeta{ID: object, Scope: owner.Scope(), MediaType: "text/plain", ByteSize: 1, SHA256: foundation.Digest("sha256:" + strings.Repeat("a", 64)), State: Available, Version: 1}
	ref, e := NewBusinessFileRef(BusinessFileDetails{Kind: ArtifactFile, ProjectID: project, ArtifactID: owner.Details().ID, FileID: secondID})
	if e != nil {
		t.Fatal(e)
	}
	source, e := NewResolvedSource(ResolvedSourceDetails{Reference: ref, Owner: owner, Meta: meta, Revision: 1})
	if e != nil {
		t.Fatal(e)
	}
	acquire, e := NewSourceAccess(AccessRequestDetails{Operation: AcquireSourceAccess, Actor: actor, Source: source})
	if e != nil {
		t.Fatal(e)
	}
	validate, e := NewSourceAccess(AccessRequestDetails{Operation: ValidateSourceAccess, Actor: actor, Source: source})
	if e != nil || acquire.Equal(validate) {
		t.Fatal("shared validation substituted for lease acquisition")
	}
	d := acquire.Details()
	d.Operation = OpenSourceAccess
	if _, e = NewSourceAccess(d); e == nil {
		t.Fatal("wrapper operation accepted in source plan")
	}
}
