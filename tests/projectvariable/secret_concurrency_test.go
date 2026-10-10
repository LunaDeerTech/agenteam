//go:build integration

package projectvariable_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type secretOwnerReply struct {
	value vc.SecretVariableMutation
	err   error
}

func asyncSecretOwner(t *testing.T, release func(), fn func(context.Context) (vc.SecretVariableMutation, error)) <-chan secretOwnerReply {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan secretOwnerReply, 1)
	done := make(chan struct{})
	go func() { defer close(done); r, err := fn(ctx); result <- secretOwnerReply{r, err} }()
	t.Cleanup(func() {
		if release != nil {
			release()
		}
		cancel()
		await(t, done)
	})
	return result
}
func secretReply(t *testing.T, ch <-chan secretOwnerReply) secretOwnerReply {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("Secret Owner call did not return")
	}
	return secretOwnerReply{}
}
func awaitSecretStage(t *testing.T, stage <-chan struct{}, result <-chan secretOwnerReply) {
	t.Helper()
	select {
	case <-stage:
	case early := <-result:
		t.Fatal("Secret call ended before actual stage", early.err)
	case <-time.After(10 * time.Second):
		t.Fatal("Secret stage not reached")
	}
}
func waitSecretAttempt(t *testing.T, ch <-chan lockAttempt, result <-chan secretOwnerReply) lockAttempt {
	t.Helper()
	select {
	case a := <-ch:
		if a.pid <= 0 {
			t.Fatal("missing actual backend PID")
		}
		return a
	case early := <-result:
		t.Fatal("Secret follower ended before actual lock", early.err)
	case <-time.After(5 * time.Second):
		t.Fatal("Secret follower never attempted exact lock")
	}
	return lockAttempt{}
}

func holdSecretFinal(t *testing.T, v *secretOwnerFixture, identity f.CommandIdentity) (<-chan struct{}, *atomic.Int32, func()) {
	t.Helper()
	reached, release := make(chan struct{}), make(chan struct{})
	pid := new(atomic.Int32)
	var armed atomic.Bool
	var once sync.Once
	v.tracked.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || armed.Load() {
			return nil
		}
		x, err := v.tracked.InTx(tx)
		if err != nil {
			return err
		}
		var complete bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3)`, v.project.ID.String(), identity.Command(), string(identity.Key())).Scan(&complete); err != nil {
			return err
		}
		if !complete || !armed.CompareAndSwap(false, true) {
			return nil
		}
		var backend int32
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backend); err != nil {
			return err
		}
		pid.Store(backend)
		close(reached)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unlock(); v.tracked.setAfter(nil) })
	return reached, pid, unlock
}

// Discovery always requests these three keys. Choose the first actual sorted
// intersection, rather than assuming every race first blocks on CommandLock.
func secretFirstConflict(t *testing.T, a i.Actor, p vc.ProjectID, left, right f.CommandIdentity) f.LockKey {
	t.Helper()
	requests := func(command f.CommandIdentity) []f.LockRequest {
		c, err := f.CommandLock(command)
		if err != nil {
			t.Fatal(err)
		}
		u, err := f.UserLock(a.Details().UserID)
		if err != nil {
			t.Fatal(err)
		}
		project, err := f.ProjectLock(p.String())
		if err != nil {
			t.Fatal(err)
		}
		locks, err := oc.NormalizeLocks([]f.LockRequest{{Key: c, Mode: f.Exclusive}, {Key: u, Mode: f.Exclusive}, {Key: project, Mode: f.Exclusive}})
		if err != nil {
			t.Fatal(err)
		}
		return locks
	}
	for _, r := range requests(right) {
		for _, l := range requests(left) {
			if f.CompareLockKeys(l.Key, r.Key) == 0 {
				return r.Key
			}
		}
	}
	t.Fatal("no expected shared serialization key")
	return f.LockKey{}
}

func TestSecretVariableOwnerConcurrency(t *testing.T) {
	v := newSecretOwnerFixture(t)
	a, p := v.ownerBrowser.actor, v.project.ID
	for _, kind := range []string{"same-key-original-intent", "same-key-other-value", "two-keys-update-delete-version", "ordinary-secret-name"} {
		t.Run(kind, func(t *testing.T) {
			input := secretCreateInput(t, "RACE_"+id[struct{}](t).String()[24:], []byte("first-private-value"))
			leftMeta, rightMeta := meta(t, "left-"+kind, nil), meta(t, "right-"+kind, nil)
			command, rightCommand := vc.SecretCreateCommand, vc.SecretCreateCommand
			left := func(ctx context.Context) (vc.SecretVariableMutation, error) {
				return v.owner.CreateSecretVariable(ctx, a, leftMeta, p, input)
			}
			right := func(ctx context.Context) (vc.SecretVariableMutation, error) {
				return v.owner.CreateSecretVariable(ctx, a, rightMeta, p, input)
			}
			expected := f.Code("")
			switch kind {
			case "same-key-original-intent":
				rightMeta = leftMeta
			case "same-key-other-value":
				rightMeta = leftMeta
				expected = f.IdempotencyKeyReused
				material, err := sc.NewSecretMaterial([]byte("different-private-value"))
				if err != nil {
					t.Fatal(err)
				}
				defer material.Destroy()
				other, err := vc.NewSecretVariableCreate(vc.SecretVariableCreateFields{ID: input.Fields().ID, Name: input.Fields().Name, Description: input.Fields().Description, Value: material})
				if err != nil {
					t.Fatal(err)
				}
				defer other.Destroy()
				right = func(ctx context.Context) (vc.SecretVariableMutation, error) {
					return v.owner.CreateSecretVariable(ctx, a, rightMeta, p, other)
				}
			case "two-keys-update-delete-version":
				seed, err := v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, "seed-version", nil), p, input)
				if err != nil {
					t.Fatal(err)
				}
				version := seed.Fields().Variable.Fields().Version
				leftMeta.ExpectedVersion = &version
				rightMeta.ExpectedVersion = &version
				command, rightCommand = vc.SecretUpdateCommand, vc.SecretDeleteCommand
				expected = f.VersionConflict
				description := "new-safe-description"
				patch := secretUpdateInput(t, vc.SecretVariableUpdateFields{Description: &description})
				left = func(ctx context.Context) (vc.SecretVariableMutation, error) {
					return v.owner.UpdateSecretVariable(ctx, a, leftMeta, p, input.Fields().ID, patch)
				}
				right = func(ctx context.Context) (vc.SecretVariableMutation, error) {
					return v.owner.DeleteSecretVariable(ctx, a, rightMeta, p, input.Fields().ID)
				}
			case "ordinary-secret-name":
				expected = f.ResourceBusy
				ordinary := createInput(t, input.Fields().Name, "ordinary")
				right = func(ctx context.Context) (vc.SecretVariableMutation, error) {
					_, err := v.service.CreateVariable(ctx, a, rightMeta, p, ordinary)
					return vc.SecretVariableMutation{}, err
				}
			}
			leftIdentity, err := vc.SecretVariableCommandIdentity(p, command, leftMeta.IdempotencyKey)
			if err != nil {
				t.Fatal(err)
			}
			rightIdentity, err := vc.SecretVariableCommandIdentity(p, rightCommand, rightMeta.IdempotencyKey)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "ordinary-secret-name" {
				rightIdentity, err = vc.VariableCommandIdentity(p, vc.CreateCommand, rightMeta.IdempotencyKey)
				if err != nil {
					t.Fatal(err)
				}
			}
			before := v.secretCounts(t)
			reached, pid, release := holdSecretFinal(t, v, leftIdentity)
			leader := asyncSecretOwner(t, release, left)
			awaitSecretStage(t, reached, leader)
			key := secretFirstConflict(t, a, p, leftIdentity, rightIdentity)
			observed := observeLock(v.tracked, key)
			follower := asyncSecretOwner(t, release, right)
			attempt := waitSecretAttempt(t, observed, follower)
			v.waitLock(t, attempt, false, pid.Load())
			release()
			first, last := secretReply(t, leader), secretReply(t, follower)
			if first.err != nil {
				t.Fatal("Secret leader", first.err)
			}
			if expected != "" {
				requireCode(t, last.err, expected)
			} else {
				if last.err != nil {
					t.Fatal("same original intent", last.err)
				}
				sameSecretReceipt(t, first.value, last.value)
			}
			after := v.secretCounts(t)
			for n := range before {
				if after[n] != before[n]+1 {
					t.Fatal("race produced more than one intended mutation", before, after)
				}
			}
		})
	}
}
