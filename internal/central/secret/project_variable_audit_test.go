package secret

import (
	"context"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestProjectVariableNativeAuditRequiresPrivateOriginalCallAndFacts(t *testing.T) {
	for _, name := range []string{"no-witness", "wrong-type", "old-variant-present", "other-store", "other-tx", "other-session", "missing-full-lock", "missing-minimum", "missing-receipt", "wrong-writer", "wrong-external-expected", "wrong-effect", "wrong-receipt-payload", "kind2", "other-receipt-owner", "other-current-purpose", "other-current-version", "other-current-payload", "wrong-prior", "none-effect", "value-still-live-after-delete", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			kind := sc.Update
			if name == "value-still-live-after-delete" {
				kind = sc.Delete
			}
			s, p, store, _, appender, _ := projectVariableApplyFixture(t, kind, false)
			got, err := s.ApplyProjectVariableWriteInTx(context.Background(), store.tx, p)
			if err != nil || !got.Observed() || appender.calls != 1 {
				t.Fatalf("native call setup: %v", err)
			}
			w, ok := appender.ctx.Value(projectVariableAuditWitnessKey{}).(projectVariableAuditWitness)
			if !ok {
				t.Fatal("native private witness missing")
			}
			ctx, tx := appender.ctx, store.tx
			switch name {
			case "no-witness":
				ctx = context.Background()
			case "wrong-type":
				ctx = context.WithValue(ctx, projectVariableAuditWitnessKey{}, "not a witness")
			case "old-variant-present":
				ctx = context.WithValue(ctx, projectAuditWitnessKey{}, projectAuditWitness{})
			case "other-store":
				w.store = &projectAuditUnitStore{}
			case "other-tx":
				tx = f.NewTx()
			case "other-session":
				r := w.request.Fields()
				user, _ := f.ParseID[i.User](r.Actor.Details().UserID)
				r.Actor, _ = i.NewHuman(user, projectAuditID[i.Session](t))
				w.request, _ = sc.NewProjectVariableWriteRequest(r)
			case "missing-full-lock":
				key, _ := f.SystemConfigLock("extra-call-dependency")
				w.locks = append(append([]f.LockRequest(nil), w.locks...), f.LockRequest{Key: key, Mode: f.Exclusive})
			case "missing-minimum":
				store.locks = nil
			case "missing-receipt":
				store.receipt = nil
			case "wrong-writer":
				store.receipt[3] = projectAuditID[i.User](t).String()
			case "wrong-external-expected":
				store.receipt[6] = int64(3)
			case "wrong-effect":
				store.receipt[8] = "none"
			case "wrong-receipt-payload":
				store.receipt[11] = projectAuditID[payloadMarker](t).String()
			case "kind2":
				store.payloads[w.receiptPayload.String()][3] = int16(receiptOwner)
			case "other-receipt-owner":
				store.payloads[w.receiptPayload.String()][4] = projectAuditID[sc.ProjectVariableReceipt](t).String()
			case "other-current-purpose":
				store.purpose = sc.Model
			case "other-current-version":
				store.version++
			case "other-current-payload":
				store.current = w.receiptPayload.String()
			case "wrong-prior":
				w.expected++
			case "none-effect":
				r, _ := w.result.Result()
				r.Effect = sc.ProjectVariableUnchanged
				w.result, _ = sc.NewProjectVariableWriteObservation(r)
			case "value-still-live-after-delete":
				store.current = projectAuditID[payloadMarker](t).String()
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			if name != "no-witness" && name != "wrong-type" {
				ctx = context.WithValue(ctx, projectVariableAuditWitnessKey{}, w)
			}
			if err := appender.checker.CheckProjectAuditInTx(ctx, tx, appender.entry, appender.key); err == nil {
				t.Fatal("forged/stale native audit accepted")
			}
			if strings.Contains(fmt.Sprintf("%+v %#v", w, w), "opaque-value") {
				t.Fatal("witness material output")
			}
		})
	}
}
