//go:build integration

package skill_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

// This fixture reuses the accepted full PostgreSQL frame proxy unchanged. It
// drops the selected caller before forwarding its entire original COMMIT, then
// waits for actual upstream completion. It never substitutes CommitResult.
func withCommitProxy(t *testing.T, p *skillPG) (*skillPG, *commitproxy.Proxy) {
	t.Helper()
	proxy, e := commitproxy.New(testContext(t), net.JoinHostPort("127.0.0.1", p.db.Fixture.Port))
	if e != nil {
		t.Fatal("owned commit proxy setup failed")
	}
	t.Cleanup(func() {
		proxy.Release()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := proxy.Close(ctx); e != nil {
			t.Error("owned commit proxy did not join", e)
		}
	})
	u, e := url.Parse(p.db.Fixture.URL(p.db.Name))
	if e != nil {
		t.Fatal("owned fixture URL invalid")
	}
	u.Host = proxy.Address()
	store := openSkillStore(t, p.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "CA_FILE": ""}))
	// Same pure fixture keyrings, but every Project/Skill check uses this exact
	// proxied Store. A transaction from the direct Store is never borrowed.
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	cursors, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)))
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)), cursors)
	if e != nil {
		t.Fatal(e)
	}
	downloads, e := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)), cursors, secrets)
	if e != nil {
		t.Fatal(e)
	}
	ak, e := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)), cursors, secrets, downloads)
	if e != nil {
		t.Fatal(e)
	}
	accounts, e := account.NewAuthority(store, ak)
	if e != nil {
		t.Fatal(e)
	}
	projects, e := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts})
	if e != nil {
		t.Fatal(e)
	}
	return &skillPG{p.db, store, projects}, proxy
}

type publicationCommitArm struct {
	*controlledObjects
	proxy  *commitproxy.Proxy
	writer int32
	armed  bool
}

func (o *publicationCommitArm) PublishVerifiedInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, a oc.UploadAttempt, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.PutResult, error) {
	result, e := o.controlledObjects.PublishVerifiedInTx(ctx, tx, actor, owner, a, plan, locked)
	if e != nil {
		return oc.PutResult{}, e
	}
	if o.armed {
		o.t.Fatal("original publication callback automatically retried")
	}
	x, e := o.store.InTx(tx)
	if e != nil {
		return oc.PutResult{}, e
	}
	if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&o.writer); e != nil {
		return oc.PutResult{}, e
	}
	if e = o.proxy.Arm(o.writer); e != nil {
		return oc.PutResult{}, e
	}
	o.armed = true
	return result, nil
}

func waitSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ch:
	case <-timer.C:
		t.Fatal(label)
	}
}

func TestSkillInitializationCommitRecovery(t *testing.T) {
	direct := newSkillPG(t)
	p, proxy := withCommitProxy(t, direct)
	v := p.newCase(t)
	o := &publicationCommitArm{controlledObjects: v.objects, proxy: proxy}
	bundle, e := skill.AddSkills(testContext(t))
	if e != nil {
		t.Fatal(e)
	}
	service, e := skill.New(skill.Dependencies{Authority: v.authority, Objects: o, Processes: controlledProcesses{}, ProcessID: testID[oc.Process](t), Bundle: bundle})
	if e != nil {
		t.Fatal(e)
	}
	// Cleanup order keeps the upstream writer owned until its real return; no
	// cancellation, timeout or Stop observation is interpreted as an actual join.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	done := make(chan struct{})
	type result struct {
		value pc.InitializationResult
		err   error
	}
	returned := make(chan result, 1)
	t.Cleanup(func() {
		cancel()
		proxy.Release()
		<-done
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if e := service.Drain(cleanup); e != nil {
			t.Error("original Skill call/work not joined", e)
		}
	})
	go func() {
		defer close(done)
		out, e := service.InitializeProjectSkills(ctx, v.actor, v.request)
		returned <- result{out, e}
	}()
	waitSignal(t, proxy.Reached(), "original complete COMMIT frame not held")
	var first result
	timer := time.NewTimer(5 * time.Second)
	select {
	case first = <-returned:
		timer.Stop()
	case <-timer.C:
		t.Fatal("original Unknown caller did not return")
	}
	waitSignal(t, done, "original initialization goroutine did not return")
	original, ok := skill.UnknownAttempt(first.err)
	if !ok || original.State() != f.Unknown || original.AttemptID().Validate() != nil || first.value.State != "" || proxy.WriterPID() != o.writer {
		t.Fatal("original physical commit provenance lost or invented completion")
	}
	select {
	case <-proxy.Committed():
		t.Fatal("held original commit was forwarded early")
	default:
	}
	// Other real connections see only the last committed reservation. A fresh
	// read attempting the original lock cannot turn this into a known rollback.
	var phase string
	if e = direct.store.QueryRow(testContext(t), `SELECT phase FROM agenteam_skill.initializations WHERE project_id=$1`, v.request.ProjectID.String()).Scan(&phase); e != nil || phase != "reserved" {
		t.Fatal("uncommitted publication became visible", e)
	}
	if _, e = service.InspectProjectSkills(testContext(t), v.actor, v.request); e == nil {
		t.Fatal("in-flight original writer was treated as a completed read")
	}
	before := len(v.objects.steps)
	proxy.Release()
	waitSignal(t, proxy.Committed(), "upstream original COMMIT never completed")
	waitSignal(t, proxy.HeldJoined(), "original proxy writer did not actually join")
	observed, e := service.InspectProjectSkills(testContext(t), v.actor, v.request)
	if e != nil || observed.State != pc.InitializationCompleted || !observed.Matches(v.request) || len(v.objects.steps) != before {
		t.Fatal("read-only recovery lost original publication or replayed physical work", e)
	}
	// Draining after the actual callback and upstream completion may now commit
	// its original work retirement. This does not restart or repeat publication.
	drain, stop := context.WithTimeout(context.Background(), 3*time.Second)
	e = service.Drain(drain)
	stop()
	if e != nil {
		t.Fatal("completed original work did not drain", e)
	}
	assertPublishedFacts(t, p, v, observed)
	restarted := p.newService(t, v.authority, v.objects)
	replay, e := restarted.InitializeProjectSkills(testContext(t), v.actor, v.request)
	if e != nil || replay.State != pc.InitializationCompleted || *replay.AddSkillsID != *observed.AddSkillsID || len(v.objects.steps) != before {
		t.Fatal("same original replay created another physical publication", e)
	}
	preserved, ok := skill.UnknownAttempt(first.err)
	if !ok || preserved.AttemptID() != original.AttemptID() || preserved.State() != f.Unknown {
		t.Fatal("later proof rewrote original Unknown result")
	}
}

var _ skill.Store = (*postgres.Store)(nil)
