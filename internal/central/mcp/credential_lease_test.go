package mcp

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestMCPCredentialLeaseSQLValue(t *testing.T) {
	lease, err := f.ParseID[sc.Lease]("01900000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	value, err := credentialLeaseValue("", sc.LeaseID{})
	if err != nil || value != "" || null(value) != nil {
		t.Fatal("unauthenticated lease must be SQL NULL and compare as empty text")
	}
	value, err = credentialLeaseValue("01900000-0000-7000-8000-000000000002", lease)
	if err != nil || value != "01900000-0000-7000-8000-000000000001" || null(value) != value {
		t.Fatal("authenticated lease must retain the exact valid ID")
	}
	if _, err := credentialLeaseValue("credential", sc.LeaseID{}); err == nil {
		t.Fatal("missing authenticated lease accepted")
	}
	if _, err := credentialLeaseValue("", lease); err == nil {
		t.Fatal("unauthenticated connection accepted a credential lease")
	}
}
