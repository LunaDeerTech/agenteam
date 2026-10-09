package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestConstructorAndZeroServiceFailClosed(t *testing.T) {
	var typedNil *constructorStore
	for _, store := range []Store{nil, typedNil} {
		service, err := New(store, Dependencies{})
		var known *f.Fault
		if service != nil || !errors.As(err, &known) || known.Code != f.DependencyUnbound {
			t.Fatal("unbound constructor accepted", err)
		}
	}
	var service *Service
	if _, _, err := service.begin(context.Background()); err == nil {
		t.Fatal("nil receiver accepted")
	}
	if _, _, err := (&Service{}).begin(context.Background()); err == nil {
		t.Fatal("zero receiver accepted")
	}
}

type constructorStore struct{ Store }

func TestUnknownKeepsActualAttemptAndPrivateCause(t *testing.T) {
	key := f.IdempotencyKey("private-canary-command-key")
	owner, err := f.NewID[struct{}]()
	if err != nil {
		t.Fatal(err)
	}
	command, err := f.NewCommandIdentity("knowledge", []string{owner.String()}, "create", key)
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := f.NewID[f.TransactionAttempt]()
	if err != nil {
		t.Fatal(err)
	}
	result := f.UnknownResult(attempt, cause)
	err = txError(result)
	var known *f.Fault
	var original commitFailure
	if !errors.As(err, &known) || known.Code != f.CommitUnknown || known.CauseID != attempt.String() || !errors.As(err, &original) {
		t.Fatal("unknown lost provenance", err)
	}
	if original.result.AttemptID() != attempt || original.result.Cause().Details().Kind != cause.Details().Kind {
		t.Fatal("changed physical attempt")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", err, original), string(key)) {
		t.Fatal("private command leaked")
	}
}
