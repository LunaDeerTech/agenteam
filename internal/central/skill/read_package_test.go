package skill

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type packageObjects struct {
	ObjectPorts
	t            *testing.T
	store        *initializationWriteStore
	actor        id.Actor
	row          initializationRow
	meta         sc.RevisionMetadata
	body         *packageTestBody
	mode         string
	err          error
	reads        int
	beforeReturn func()
}

func (o *packageObjects) ReadObject(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, object oc.ObjectID, wanted *oc.ByteRange) (*oc.ObjectReader, error) {
	o.reads++
	want, _ := o.row.owner()
	if ctx.Err() != nil || !actor.Equal(o.actor) || !owner.Equal(want) || object != o.meta.Object.ID || wanted != nil || o.store.live || o.store.txs != 1 || len(o.store.work) != 1 {
		o.t.Fatal("read before committed current-owner/work boundary")
	}
	if o.mode == "nil" {
		return nil, nil
	}
	m := o.meta.Object
	if o.mode == "wrong_object" {
		m.ID = stateID[oc.StoredObject](199)
	}
	var span *oc.ResolvedRange
	if o.mode == "range" {
		span = &oc.ResolvedRange{Offset: 0, Length: 1, Total: m.ByteSize}
	}
	r, e := oc.NewObjectReader(m, span, o.body)
	if e != nil {
		o.t.Fatal(e)
	}
	if o.mode == "read_error" {
		return r, o.err
	}
	if o.beforeReturn != nil {
		o.beforeReturn()
	}
	return r, nil
}

type packageTestBody struct {
	reader           *bytes.Reader
	started, release chan struct{}
	closed           chan struct{}
	closeOnce        sync.Once
	closes           atomic.Int32
	err              error
}

func (b *packageTestBody) Read(p []byte) (int, error) {
	if b.started != nil {
		close(b.started)
		<-b.release
		return 0, io.ErrClosedPipe
	}
	return b.reader.Read(p)
}
func (b *packageTestBody) Close() error {
	b.closes.Add(1)
	b.closeOnce.Do(func() { close(b.closed) })
	return b.err
}
func packageFixture(t *testing.T) (*Service, *initializationWriteStore, *skillReaderProjects, *packageObjects) {
	t.Helper()
	s, base, projects, actor, _ := readerFixture(t)
	store := &initializationWriteStore{initializationReadStore: base}
	s.state().authority.state().store = store
	row, e := loadInitialization(context.Background(), store, projects.project.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, meta, e := loadPublished(context.Background(), store, *row)
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.state().bundle.Package()
	if e != nil {
		t.Fatal(e)
	}
	data, e := p.Bytes()
	if e != nil {
		t.Fatal(e)
	}
	body := &packageTestBody{reader: bytes.NewReader(data), closed: make(chan struct{})}
	objects := &packageObjects{t: t, store: store, actor: actor, row: *row, meta: meta, body: body, err: errors.New("controlled object failure")}
	s.state().objects = objects
	return s, store, projects, objects
}
func waitPackageCalls(t *testing.T, s *Service) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		st := s.state()
		st.mu.Lock()
		n := len(st.calls)
		changed := st.changed
		st.mu.Unlock()
		if n == 0 {
			return
		}
		select {
		case <-changed:
		case <-timer.C:
			t.Fatal("actual reader tail not returned")
		}
	}
}

func TestSkillPackageReaderCurrentGateCommitAndMetadata(t *testing.T) {
	for _, name := range []string{"success", "wrong_revision", "denied", "registration_unknown", "read_error", "nil", "wrong_object", "range", "close_error", "retirement_unknown"} {
		t.Run(name, func(t *testing.T) {
			s, store, projects, o := packageFixture(t)
			revision := f.Revision(1)
			switch name {
			case "wrong_revision":
				revision = 2
			case "denied":
				projects.err = fault(f.Forbidden)
			case "registration_unknown":
				store.unknownAt = 1
			case "retirement_unknown":
				store.unknownAt = 2
			case "close_error":
				o.body.err = o.err
			default:
				o.mode = name
			}
			r, e := s.OpenPackage(context.Background(), o.actor, o.row.request.ProjectID, o.row.skill, revision)
			success := name == "success" || name == "close_error" || name == "retirement_unknown"
			if !success {
				if e == nil || r != nil {
					t.Fatal("invalid reader escaped")
				}
				if name == "denied" && e != projects.err {
					t.Fatal("gate error identity")
				}
				if name == "read_error" && e != o.err {
					t.Fatal("object error identity")
				}
				if name == "denied" || name == "wrong_revision" || name == "registration_unknown" {
					if o.reads != 0 {
						t.Fatal("premature body request")
					}
				}
				if name == "read_error" || name == "wrong_object" || name == "range" {
					if o.body.closes.Load() != 1 {
						t.Fatal("rejected body not closed once")
					}
				}
			} else {
				if e != nil || r == nil {
					t.Fatal("open", e)
				}
				got, e := io.ReadAll(r)
				if e != nil || sum(got) != o.meta.PackageSHA256 {
					t.Fatal("actual builtin read", e)
				}
				st := s.state()
				st.mu.Lock()
				pending := len(st.calls) == 1 && len(st.work) == 1
				st.mu.Unlock()
				if !pending || r.Joined() {
					t.Fatal("EOF pretended to close")
				}
				e = r.Close()
				if name == "success" && e != nil {
					t.Fatal(e)
				}
				if name == "close_error" && e != o.err {
					t.Fatal("close error identity")
				}
				if name == "retirement_unknown" {
					if _, ok := UnknownAttempt(e); !ok {
						t.Fatal("retirement Unknown lost", e)
					}
				}
				if !r.Joined() || o.body.closes.Load() != 1 {
					t.Fatal("local reader close accounting")
				}
			}
			s.Stop()
			if name == "registration_unknown" || name == "retirement_unknown" {
				if s.Joined() {
					t.Fatal("unconfirmed durable tail joined")
				}
				if e := s.Drain(context.Background()); e != nil {
					t.Fatal(e)
				}
			}
			if !s.Joined() {
				t.Fatal("returned local work not joined")
			}
		})
	}
}

func TestSkillPackageReaderCancelAndCloseDoNotJoinBlockedRead(t *testing.T) {
	s, _, _, o := packageFixture(t)
	o.body.started = make(chan struct{})
	o.body.release = make(chan struct{})
	r, e := s.OpenPackage(context.Background(), o.actor, o.row.request.ProjectID, o.row.skill, 1)
	if e != nil {
		t.Fatal(e)
	}
	readDone := make(chan error, 1)
	go func() { _, e := r.Read(make([]byte, 1)); readDone <- e }()
	<-o.body.started
	s.Stop()
	<-o.body.closed
	if s.Joined() {
		t.Fatal("cancellation joined live Read")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(s.Drain(ctx), context.Canceled) {
		t.Fatal("borrowed drain budget")
	}
	close(o.body.release)
	if e := <-readDone; !errors.Is(e, io.ErrClosedPipe) {
		t.Fatal(e)
	}
	waitPackageCalls(t, s)
	if s.Joined() {
		t.Fatal("cancelled database tail pretended committed")
	}
	if e := s.Drain(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !s.Joined() || o.body.closes.Load() != 1 {
		t.Fatal("actual join")
	}
	if e := r.Close(); e == nil {
		t.Fatal("original cancelled accounting result lost")
	}
	if !r.Joined() {
		t.Fatal("P1 local join after actual returns")
	}
}

func TestSkillPackageReaderCancellationBeforeOwnershipTransfer(t *testing.T) {
	s, _, _, o := packageFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o.beforeReturn = cancel
	r, e := s.OpenPackage(ctx, o.actor, o.row.request.ProjectID, o.row.skill, 1)
	if r != nil || !errors.Is(e, context.Canceled) {
		t.Fatal("cancelled body escaped", e)
	}
	waitPackageCalls(t, s)
	s.Stop()
	if o.body.closes.Load() != 1 || s.Joined() {
		t.Fatal("callback/registration tail lost")
	}
	if e := s.Drain(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !s.Joined() {
		t.Fatal("actual callback return not retired")
	}
}
