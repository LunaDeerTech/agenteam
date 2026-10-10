package projectvariable

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func secretPlanBefore(t *testing.T) (*secretVariableRow, *secretCommandRecord) {
	t.Helper()
	r := secretStoredRecord(t, c.SecretUpdateCommand, false, false)
	o, _ := r.Observation.Result()
	return &secretVariableRow{Variable: r.Receipt.Fields().Variable, Ref: o.Ref, CredentialVersion: o.Version}, r
}
func TestSecretOwnerPlanExplicitValueMetadataNoopAndVersionLimits(t *testing.T) {
	before, r := secretPlanBefore(t)
	request := secretAuthorityRequest(t, r, 30)
	at, _ := f.ParseInstant("2026-10-09T10:00:01Z")
	material, err := sc.NewSecretMaterial([]byte("private-original-value"))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	for _, name := range []string{"noop", "metadata", "replace", "all", "delete"} {
		t.Run(name, func(t *testing.T) {
			n, description := before.Variable.Fields().Name, before.Variable.Fields().Description
			fields := c.SecretVariableUpdateFields{Name: &n}
			if name == "metadata" || name == "all" {
				description = "next-description"
				fields.Description = &description
			}
			if name == "replace" || name == "all" {
				fields.Value = &material
			}
			if name == "all" {
				n = "NEXT_CONFIG"
			}
			update, err := c.NewSecretVariableUpdate(fields)
			if err != nil {
				t.Fatal(err)
			}
			defer update.Destroy()
			req := request
			var intent sc.ProjectVariableIntent
			if name == "delete" {
				d := *r
				d.Command = c.SecretDeleteCommand
				req = secretAuthorityRequest(t, &d, 30)
				intent, err = secretIntent(req, nil, nil)
			} else {
				intent, err = secretIntent(req, nil, &update)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer intent.Destroy()
			plan, changed, err := planSecretVariable(intent, before, at, testID[c.Operation](40), testID[c.Operation](41))
			if err != nil {
				t.Fatal(err)
			}
			wantFields := map[string]string{"noop": "", "metadata": "description", "replace": "value", "all": "description,name,value", "delete": "deleted"}[name]
			if changed != (name != "noop") || strings.Join(plan.Fields, ",") != wantFields || plan.Deleted != (name == "delete") {
				t.Fatal("wrong explicit semantic")
			}
			wantVersion := before.Variable.Fields().Version
			if changed {
				wantVersion++
			}
			if plan.After.Fields().Version != wantVersion || before.Variable.Fields().Version != 7 || before.CredentialVersion != 2 {
				t.Fatal("version domains mixed")
			}
			if !changed && plan.After.Fields().UpdatedAt != before.Variable.Fields().UpdatedAt {
				t.Fatal("no-op changed clock")
			}
			copy := *plan
			if !secretPlanSame(plan, &copy) {
				t.Fatal("same plan rejected")
			}
			copy.Operation = testID[c.Operation](42)
			if secretPlanSame(plan, &copy) {
				t.Fatal("opaque JSON marker compared as plan equality")
			}
			if strings.Contains(fmt.Sprintf("%#v", plan), "private-original-value") || strings.Contains(fmt.Sprintf("%#v", plan), "next-description") {
				t.Fatal("plan formatted metadata/material")
			}
		})
	}
	fields := before.Variable.Fields()
	fields.Version = f.Version(math.MaxInt64)
	before.Variable, _ = c.NewSecretVariable(fields)
	r.Expected = &fields.Version
	request = secretAuthorityRequest(t, r, 30)
	name := fields.Name
	update, _ := c.NewSecretVariableUpdate(c.SecretVariableUpdateFields{Name: &name})
	defer update.Destroy()
	intent, _ := secretIntent(request, nil, &update)
	defer intent.Destroy()
	if _, changed, err := planSecretVariable(intent, before, at, testID[c.Operation](40), testID[c.Operation](41)); err != nil || changed {
		t.Fatal("max-version no-op failed", err)
	}
	replace, _ := c.NewSecretVariableUpdate(c.SecretVariableUpdateFields{Value: &material})
	defer replace.Destroy()
	replacement, _ := secretIntent(request, nil, &replace)
	defer replacement.Destroy()
	if _, _, err = planSecretVariable(replacement, before, at, testID[c.Operation](40), testID[c.Operation](41)); err == nil {
		t.Fatal("version overflow accepted")
	}
}
func TestSecretOwnerIntentOwnsBytesAndOriginalSession(t *testing.T) {
	r := secretStoredRecord(t, c.SecretCreateCommand, true, true)
	request := secretAuthorityRequest(t, r, 30)
	material, _ := sc.NewSecretMaterial([]byte("independent-material-canary"))
	defer material.Destroy()
	create, err := c.NewSecretVariableCreate(c.SecretVariableCreateFields{ID: r.Target, Name: "CONFIG", Description: "safe", Value: material})
	if err != nil {
		t.Fatal(err)
	}
	defer create.Destroy()
	intent, err := secretIntent(request, &create, nil)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Fields().Request.Fields().Actor.Details().SessionID != testID[i.Session](30).String() {
		t.Fatal("Session dropped")
	}
	intent.Destroy()
	if intent.UseValue(func([]byte) error { return nil }) == nil {
		t.Fatal("destroyed intent still usable")
	}
	if err = create.UseValue(func(v []byte) error {
		if string(v) != "independent-material-canary" {
			t.Fatal("caller value modified")
		}
		return nil
	}); err != nil {
		t.Fatal("caller request retired", err)
	}
	intent, err = secretIntent(request, &create, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer intent.Destroy()
	create.Destroy()
	if err = intent.UseValue(func(v []byte) error {
		if string(v) != "independent-material-canary" {
			t.Fatal("intent lost owned bytes")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	at, _ := f.ParseInstant("2026-10-09T10:00:00Z")
	plan, changed, err := planSecretVariable(intent, nil, at, testID[c.Operation](40), testID[c.Operation](41))
	if err != nil || !changed || plan.After.Fields().Version != 1 || strings.Join(plan.Fields, ",") != "created" {
		t.Fatal("create plan", err)
	}
}
func TestSecretOwnerCallLifetimeJoinsAfterStopAndRejectsAdmission(t *testing.T) {
	st := &secretServiceState{calls: map[*call]struct{}{}, changed: make(chan struct{})}
	s := &SecretService{data: func() *secretServiceState { return st }}
	ctx, entry, done, err := s.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	confirmationCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st.mu.Lock()
	entry.confirmations[&confirmation{cancel: cancel}] = struct{}{}
	st.mu.Unlock()
	s.Stop()
	if ctx.Err() != context.Canceled || confirmationCtx.Err() != context.Canceled {
		t.Fatal("calls not cancelled")
	}
	bounded, cancelBounded := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancelBounded()
	if s.Drain(bounded) != context.DeadlineExceeded {
		t.Fatal("cancel incorrectly treated as completion")
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); done() }()
	}
	wg.Wait()
	if err = s.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = s.begin(context.Background())
	code(t, err, f.ShuttingDown)
	if _, err = NewSecret(nil, SecretDependencies{}); err == nil {
		t.Fatal("missing dependencies accepted")
	}
}
