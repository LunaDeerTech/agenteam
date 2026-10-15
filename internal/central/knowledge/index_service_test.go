package knowledge

import (
	"context"
	"errors"
	"testing"
)

func TestIndexJoinRequiresOriginalAdmissionReturn(t *testing.T) {
	s := &IndexService{owners: map[*indexOperation]struct{}{}, calls: map[*indexAdmission]struct{}{}, changed: make(chan struct{})}
	run, done, err := s.admit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.StopAdmission()
	if !errors.Is(run.Err(), context.Canceled) || s.Joined() {
		t.Fatal("cancellation mistaken for original return")
	}
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Drain(ended); !errors.Is(err, context.Canceled) {
		t.Fatal("drain retired an active original caller", err)
	}
	if _, _, err := s.admit(context.Background()); err == nil {
		t.Fatal("stopped service readmitted")
	}
	done()
	done()
	if err := s.Drain(context.Background()); err != nil || !s.Joined() {
		t.Fatal("original return did not join", err)
	}
}
