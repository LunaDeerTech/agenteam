package contract_test

import (
	"errors"
	"math"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestSpecRefScalarContract(t *testing.T) {
	id, err := foundation.ParseID[identity.Tool]("01902e15-1000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []foundation.Version{1, math.MaxInt64} {
		ref := contract.SpecRef{ToolID: id, SpecRevision: version}
		if err := ref.Validate(); err != nil || ref.SpecRevision != version {
			t.Fatalf("valid scalar reference: %v", err)
		}
	}
	for _, ref := range []contract.SpecRef{
		{}, {ToolID: id}, {ToolID: id, SpecRevision: -1}, {SpecRevision: 1},
	} {
		var fault *foundation.Fault
		if err := ref.Validate(); !errors.As(err, &fault) || fault.Code != foundation.InvalidArgument || fault.CommitState != foundation.NotStarted {
			t.Fatalf("invalid scalar reference: %v", err)
		}
	}
}
