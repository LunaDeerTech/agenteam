package meeting

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestMeetingDeletionProjectFactsRequireOriginalLiveRuntimeTransaction(t *testing.T) {
	store := &deletionRejectStore{}
	authority := &DeletionAuthority{original: &Authority{store: store}, guard: &object.ProcessGuard{}, process: summaryTestID[oc.Process](t)}
	runtime := &Runtime{store: store, deps: RuntimeDependencies{Authority: authority.original, DeletionAuthority: authority}}
	tx := f.NewTx()
	if err := authority.RequireMeetingDeletionBindingInTx(context.Background(), tx, store); err == nil {
		t.Fatal("public transaction became an accepted deletion proof")
	}
	for _, change := range []string{"expired-runtime", "expired-transaction", "foreign-tx", "foreign-store", "foreign-authority"} {
		t.Run(change, func(t *testing.T) {
			owner := &deletionRuntimeContext{runtime: runtime, original: deletionTestRecord(t), active: true}
			w := &deletionProjectWitness{owner: owner, tx: tx, active: true}
			var requestedStore any = store
			candidate := authority
			switch change {
			case "expired-runtime":
				owner.active = false
			case "expired-transaction":
				w.active = false
			case "foreign-tx":
				w.tx = f.NewTx()
			case "foreign-store":
				requestedStore = &deletionRejectStore{}
			case "foreign-authority":
				copy := *authority
				candidate = &copy
			}
			if err := candidate.RequireMeetingDeletionBindingInTx(context.WithValue(context.Background(), deletionProjectKey{}, w), tx, requestedStore); err == nil {
				t.Fatal("foreign or expired cleanup witness reached Project convergence")
			}
		})
	}
	if store.borrowed != 0 {
		t.Fatal("untrusted cleanup evidence reached SQL")
	}
}
