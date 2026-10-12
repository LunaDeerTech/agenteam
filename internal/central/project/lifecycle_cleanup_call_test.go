package project

import (
	"context"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestLifecycleCleanupCallLifetime(t *testing.T) {
	a := lifecycleTestAuthority(t) // Any SQL access panics.
	p := testID[i.Project](t)
	ctx, cancel := context.WithCancel(context.Background())
	release, err := a.beginCleanupCall(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	cancel()
	_, err = a.beginCleanupCall(context.Background(), p)
	hasCode(t, err, f.ResourceBusy) // Cancellation is not the original return.
	other, err := a.beginCleanupCall(context.Background(), testID[i.Project](t))
	if err != nil {
		t.Fatal(err)
	}
	other()
	release()
	next, err := a.beginCleanupCall(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(next)
	release() // An old release cannot retire a later call on the same Project.
	_, err = a.beginCleanupCall(context.Background(), p)
	hasCode(t, err, f.ResourceBusy)
	next()
	if len(a.cleanupCalls.active) != 0 {
		t.Fatal("returned calls retained")
	}
}

func TestLifecycleCleanupCallBoundedAdmission(t *testing.T) {
	a := lifecycleTestAuthority(t)
	ctx := context.Background()
	releases := make([]func(), 0, 4)
	for n := 0; n < 4; n++ {
		release, err := a.beginCleanupCall(ctx, testID[i.Project](t))
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
		t.Cleanup(release)
	}
	p := testID[i.Project](t)
	_, err := a.beginCleanupCall(ctx, p)
	hasCode(t, err, f.ResourceBusy)
	releases[0]()
	release, err := a.beginCleanupCall(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	release()
	for _, done := range releases {
		done()
	}
	if len(a.cleanupCalls.active) != 0 {
		t.Fatal("capacity was not returned")
	}
}

func TestLifecycleCleanupCallConcurrentAdmission(t *testing.T) {
	a := lifecycleTestAuthority(t)
	p := testID[i.Project](t)
	start := make(chan struct{})
	type result struct {
		release func()
		err     error
	}
	results := make(chan result, 8)
	var joined sync.WaitGroup
	for n := 0; n < 8; n++ {
		joined.Add(1)
		go func() {
			defer joined.Done()
			<-start
			release, err := a.beginCleanupCall(context.Background(), p)
			results <- result{release, err}
		}()
	}
	close(start)
	joined.Wait()
	close(results)
	accepted := 0
	for r := range results {
		if r.err == nil {
			accepted++
			t.Cleanup(r.release)
		} else {
			hasCode(t, r.err, f.ResourceBusy)
		}
	}
	if accepted != 1 {
		t.Fatal("concurrent Project admitted", accepted)
	}
}

func TestLifecycleCleanupCallRejectsInvalidInput(t *testing.T) {
	a := lifecycleTestAuthority(t)
	p := testID[i.Project](t)
	_, err := a.beginCleanupCall(nil, p)
	hasCode(t, err, f.InvalidArgument)
	_, err = a.beginCleanupCall(context.Background(), c.ProjectID{})
	hasCode(t, err, f.InvalidArgument)
	var absent *LifecycleAuthority
	_, err = absent.beginCleanupCall(context.Background(), p)
	hasCode(t, err, f.DependencyUnbound)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if release, err := a.beginCleanupCall(ctx, p); err == nil || release != nil {
		t.Fatal("canceled call admitted")
	}
	if len(a.cleanupCalls.active) != 0 {
		t.Fatal("rejected call retained")
	}
}
