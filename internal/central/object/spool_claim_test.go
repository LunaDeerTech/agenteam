package object

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestSpoolInitialClaimRecoversEveryPartialManifestPrefix(t *testing.T) {
	previous, _ := foundation.NewID[oc.Process]()
	current, _ := foundation.NewID[oc.Process]()
	payload, _ := foundation.NewID[oc.Payload]()
	manifest := spoolManifest{Format: 1, ProcessID: previous, PayloadID: payload, Length: 17}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	// These are exact durable intermediate states, not simulated SIGKILL claims.
	for cut := -1; cut <= len(raw); cut++ {
		t.Run(fmt.Sprintf("prefix_%d", cut), func(t *testing.T) {
			dir := t.TempDir()
			before, err := OpenSpool(dir, previous)
			if err != nil {
				t.Fatal(err)
			}
			if err = before.initialClaim(manifest); err != nil {
				t.Fatal(err)
			}
			if cut >= 0 {
				file, err := before.state().root.OpenFile(payload.String()+".json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = file.Write(raw[:cut]); err != nil {
					t.Fatal(err)
				}
				if err = file.Sync(); err != nil {
					t.Fatal(err)
				}
				_ = file.Close()
				if err = before.syncDirectory(); err != nil {
					t.Fatal(err)
				}
			}
			if err = before.Close(); err != nil {
				t.Fatal(err)
			}
			after, err := OpenSpool(dir, current)
			if err != nil {
				t.Fatal("normal initial-write window prevents recovery", err)
			}
			defer after.Close()
			calls := 0
			proof := stoppedProcess(func(_ context.Context, id oc.ProcessID) error {
				if id != previous {
					t.Fatal("wrong process requested")
				}
				calls++
				return failure(foundation.ResourceBusy, nil)
			})
			if err = after.RecoverOrphans(context.Background(), proof); !codeIs(err, foundation.ResourceBusy) {
				t.Fatal("unconfirmed process reclaimed", err)
			}
			if _, err = os.Lstat(filepath.Join(dir, claimName(payload, previous, true))); err != nil {
				t.Fatal("claim removed before proof", err)
			}
			proof = stoppedProcess(func(_ context.Context, id oc.ProcessID) error {
				if id != previous {
					t.Fatal("wrong process requested")
				}
				calls++
				return nil
			})
			if err = after.RecoverOrphans(context.Background(), proof); err != nil {
				t.Fatal(err)
			}
			if err = after.RecoverOrphans(context.Background(), proof); err != nil || calls != 2 {
				t.Fatal("repeated recovery did not converge", err, calls)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != ".object.lock" {
				t.Fatal("owned recovery residue", err)
			}
		})
	}
}
func TestSpoolClaimsDoNotAdoptDamagedOrUnownedFiles(t *testing.T) {
	for _, scenario := range []string{"wrong_json", "wrong_identity_prefix", "ready_claim_partial", "both_claims", "body_before_initial", "next_before_initial", "no_claim", "wrong_claim_mode", "claim_symlink", "claim_only_body"} {
		t.Run(scenario, func(t *testing.T) {
			previous, _ := foundation.NewID[oc.Process]()
			current, _ := foundation.NewID[oc.Process]()
			payload, _ := foundation.NewID[oc.Payload]()
			dir := t.TempDir()
			before, err := OpenSpool(dir, previous)
			if err != nil {
				t.Fatal(err)
			}
			manifest := spoolManifest{Format: 1, ProcessID: previous, PayloadID: payload, Length: 1}
			if err = before.initialClaim(manifest); err != nil {
				t.Fatal(err)
			}
			if err = before.Close(); err != nil {
				t.Fatal(err)
			}
			raw := []byte(`{"format":1,"process_id":"`)
			claim := filepath.Join(dir, claimName(payload, previous, true))
			write := func(name string, b []byte) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "wrong_json":
				raw = []byte(`{"unexpected":`)
			case "wrong_identity_prefix":
				raw = []byte(`{"format":1,"process_id":"` + current.String() + `","payload_id":"`)
			case "ready_claim_partial":
				if err = os.Rename(claim, filepath.Join(dir, claimName(payload, previous, false))); err != nil {
					t.Fatal(err)
				}
			case "both_claims":
				write(claimName(payload, previous, false), nil)
			case "body_before_initial":
				write(payload.String()+".payload", []byte("a"))
			case "next_before_initial":
				write(payload.String()+".json.next", nil)
			case "no_claim":
				if err = os.Remove(claim); err != nil {
					t.Fatal(err)
				}
			case "wrong_claim_mode":
				if err = os.Chmod(claim, 0644); err != nil {
					t.Fatal(err)
				}
			case "claim_symlink":
				if err = os.Remove(claim); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "outside")
				if err = os.WriteFile(outside, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(outside, claim); err != nil {
					t.Fatal(err)
				}
			case "claim_only_body":
				write(payload.String()+".payload", []byte("a"))
			}
			if scenario != "claim_only_body" {
				write(payload.String()+".json", raw)
			}
			if after, err := OpenSpool(dir, current); err == nil {
				_ = after.Close()
				t.Fatal("unsafe claim state adopted")
			}
		})
	}
}
func TestSpoolReadyClaimOnlyAndLegacyManifestRecovery(t *testing.T) {
	for _, stage := range []string{"ready_claim_only", "legacy_manifest"} {
		t.Run(stage, func(t *testing.T) {
			previous, _ := foundation.NewID[oc.Process]()
			current, _ := foundation.NewID[oc.Process]()
			payload, _ := foundation.NewID[oc.Payload]()
			dir := t.TempDir()
			before, err := OpenSpool(dir, previous)
			if err != nil {
				t.Fatal(err)
			}
			m := spoolManifest{Format: 1, ProcessID: previous, PayloadID: payload, Length: 0}
			if err = before.writeManifest(m); err != nil {
				t.Fatal(err)
			}
			if err = before.Close(); err != nil {
				t.Fatal(err)
			}
			remove := payload.String() + ".json"
			if stage == "legacy_manifest" {
				remove = claimName(payload, previous, false)
			}
			if err = os.Remove(filepath.Join(dir, remove)); err != nil {
				t.Fatal(err)
			}
			after, err := OpenSpool(dir, current)
			if err != nil {
				t.Fatal(err)
			}
			defer after.Close()
			if err = after.RecoverOrphans(context.Background(), stoppedProcess(func(_ context.Context, id oc.ProcessID) error {
				if id != previous {
					t.Fatal("wrong death proof")
				}
				return nil
			})); err != nil {
				t.Fatal(err)
			}
			entries, _ := os.ReadDir(dir)
			for _, entry := range entries {
				if !strings.HasPrefix(entry.Name(), ".object.lock") {
					t.Fatal("residue remains")
				}
			}
		})
	}
}
