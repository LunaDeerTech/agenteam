package skillhttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type packagePorts struct {
	*testPorts
	open func(context.Context, id.Actor, id.ProjectID, sc.SkillID, f.Revision) (*sc.PackageReader, error)
}

func (p *packagePorts) OpenPackage(ctx context.Context, a id.Actor, project id.ProjectID, target sc.SkillID, revision f.Revision) (*sc.PackageReader, error) {
	p.calls++
	return p.open(ctx, a, project, target, revision)
}

func packageInput(t *testing.T) (sc.RevisionMetadata, []byte) {
	t.Helper()
	bundle, err := skill.AddSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pkg, _ := bundle.Package()
	raw, _ := pkg.Bytes()
	manifest, _ := pkg.Manifest()
	entry, _ := pkg.Entry()
	digest, _ := pkg.Digest()
	scope, _ := id.InProject(testMetadata().ProjectID)
	at, _ := f.ParseInstant("2026-10-11T00:00:00Z")
	meta := sc.RevisionMetadata{ID: testID[sc.Revision](20), SkillID: testMetadata().ID, ProjectID: testMetadata().ProjectID, Revision: 1, Name: entry.Name, Description: entry.Description, EntryPath: sc.EntryPath, Manifest: manifest, PackageSHA256: digest, PublishedAt: at,
		Object: oc.ObjectMeta{ID: testID[oc.StoredObject](21), Scope: scope, MediaType: sc.PackageMediaType, ByteSize: f.Progress(len(raw)), SHA256: digest, State: oc.Available, Version: 1, CreatedAt: at}}
	if err := meta.Validate(); err != nil {
		t.Fatal("package test metadata", err)
	}
	return meta, raw
}

func packageReader(t *testing.T, meta sc.RevisionMetadata, body io.ReadCloser) *sc.PackageReader {
	t.Helper()
	object, err := oc.NewObjectReader(meta.Object, nil, body)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := sc.NewPackageReader(meta, object)
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func packagePath() string {
	return testPath("/skills/" + testMetadata().ID.String() + "/revisions/1/package")
}

func TestSkillOwnerPackageHTTPCompleteGETAndHEAD(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			meta, raw := packageInput(t)
			body := &testBody{Reader: bytes.NewReader(raw)}
			reader := packageReader(t, meta, body)
			ports := &packagePorts{testPorts: &testPorts{}}
			ports.open = func(ctx context.Context, a id.Actor, p id.ProjectID, s sc.SkillID, v f.Revision) (*sc.PackageReader, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > readBudget || !a.Equal(testActor()) || p != meta.ProjectID || s != meta.SkillID || v != 1 {
					t.Fatal("current original call identity or inherited budget lost")
				}
				return reader, nil
			}
			h := &handler{ports, &testBoundary{}}
			w := newTestWriter()
			w.write = func(p []byte) (int, error) {
				if !reader.Joined() || body.closes.Load() != 1 {
					t.Fatal("package published before original Close")
				}
				return w.ResponseRecorder.Write(p)
			}
			if !HandlesPath(packagePath()) || serveTest(h, httptest.NewRequest(method, packagePath(), nil), w) || w.Code != 200 || ports.calls != 1 {
				t.Fatal("complete read failed", w.Code, w.Body.String())
			}
			for name, want := range map[string]string{"Content-Type": "application/zip", "Content-Disposition": `attachment; filename="skill-package.zip"`, "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Content-Length": strconv.Itoa(len(raw))} {
				if w.Header().Get(name) != want {
					t.Fatal("package header", name, w.Header().Get(name))
				}
			}
			if method == http.MethodGet && !bytes.Equal(w.Body.Bytes(), raw) || method == http.MethodHead && w.Body.Len() != 0 || body.closes.Load() != 1 || !reader.Joined() {
				t.Fatal("original bytes/HEAD or actual reader Close lost")
			}
			w.cleared(t)
		})
	}
}

func TestSkillOwnerPackageHTTPIntegrityAndCloseFailures(t *testing.T) {
	for _, name := range []string{"truncated", "extra", "digest", "noncanonical", "manifest", "entry", "scope", "revision", "read", "close", "unknown-close", "open-and-reader-error"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(name+"/"+method, func(t *testing.T) {
				meta, raw := packageInput(t)
				body := &testBody{}
				var openErr error
				switch name {
				case "truncated":
					raw = raw[:len(raw)-1]
				case "extra":
					raw = append(raw, 0)
				case "digest":
					raw[len(raw)/2] ^= 1
				case "noncanonical":
					raw = append(raw, 0)
					digest := sha256.Sum256(raw)
					meta.PackageSHA256 = f.Digest("sha256:" + hex.EncodeToString(digest[:]))
					meta.Object.SHA256, meta.Object.ByteSize = meta.PackageSHA256, f.Progress(len(raw))
				case "manifest":
					detail, _ := meta.Manifest.Details()
					detail.Files[0].SHA256 = f.Digest("sha256:" + strings.Repeat("0", 64))
					meta.Manifest, _ = sc.NewManifest(detail.Files)
				case "entry":
					meta.Name = "Other Skill"
				case "scope":
					meta.ProjectID = testID[id.Project](99)
					meta.Object.Scope, _ = id.InProject(meta.ProjectID)
				case "revision":
					meta.Revision = 2
				case "read":
					body.Reader = errorReader{}
				case "close":
					body.close = func() error { return errors.New("private-close-canary") }
				case "unknown-close":
					body.close = func() error { return f.NewFault(f.CommitUnknown, f.Unknown) }
				case "open-and-reader-error":
					openErr = f.NewFault(f.SessionRevoked, f.NotStarted)
				}
				if body.Reader == nil {
					body.Reader = bytes.NewReader(raw)
				}
				reader := packageReader(t, meta, body)
				ports := &packagePorts{testPorts: &testPorts{}, open: func(context.Context, id.Actor, id.ProjectID, sc.SkillID, f.Revision) (*sc.PackageReader, error) {
					return reader, openErr
				}}
				boundary := &testBoundary{}
				w := newTestWriter()
				if serveTest(&handler{ports, boundary}, httptest.NewRequest(method, packagePath(), nil), w) || w.Code == 200 || w.Header().Get("Content-Type") != "application/problem+json" || body.closes.Load() != 1 || !reader.Joined() {
					t.Fatal("failure was published or reader not retired", w.Code)
				}
				if method == http.MethodHead && w.Body.Len() != 0 || strings.Contains(w.Body.String(), "private-close-canary") || w.Header().Get("Content-Disposition") != "" {
					t.Fatal("failure leaked package/private data")
				}
				if name == "unknown-close" {
					var fault *f.Fault
					if !errors.As(boundary.problem, &fault) || fault.CommitState != f.Unknown {
						t.Fatal("close Unknown changed")
					}
				}
			})
		}
	}
}

func TestSkillOwnerPackageHTTPBoundary(t *testing.T) {
	for _, name := range []string{"revision-zero", "revision-leading-zero", "revision-overflow", "query", "empty-query", "range", "if-range", "encoding", "body", "post", "unauthenticated"} {
		t.Run(name, func(t *testing.T) {
			path, method, data, status := packagePath(), "GET", "", 400
			boundary := &testBoundary{}
			switch name {
			case "revision-zero":
				path = strings.Replace(path, "/revisions/1/", "/revisions/0/", 1)
			case "revision-leading-zero":
				path = strings.Replace(path, "/revisions/1/", "/revisions/01/", 1)
			case "revision-overflow":
				path = strings.Replace(path, "/revisions/1/", "/revisions/9223372036854775808/", 1)
			case "query":
				path += "?private=value"
			case "empty-query":
				path += "?"
			case "body":
				data = "x"
			case "post":
				method, status = "POST", 405
			case "unauthenticated":
				method, status = "POST", 401
				boundary.check = func(*http.Request) error { return f.NewFault(f.Unauthenticated, f.NotStarted) }
			}
			r := httptest.NewRequest(method, path, strings.NewReader(data))
			switch name {
			case "range":
				r.Header.Set("Range", "bytes=0-1")
			case "if-range":
				r.Header.Set("If-Range", "x")
			case "encoding":
				r.Header.Set("Content-Encoding", "identity")
			}
			ports := &packagePorts{testPorts: &testPorts{}, open: func(context.Context, id.Actor, id.ProjectID, sc.SkillID, f.Revision) (*sc.PackageReader, error) {
				t.Fatal("invalid request reached original service")
				return nil, nil
			}}
			w := newTestWriter()
			if serveTest(&handler{ports, boundary}, r, w) || w.Code != status || ports.calls != 0 {
				t.Fatal("boundary", w.Code, status)
			}
		})
	}
}

func TestSkillOwnerPackageHTTPCancelWaitsOriginalClose(t *testing.T) {
	meta, raw := packageInput(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var closed atomic.Bool
	body := &testBody{Reader: bytes.NewReader(raw), close: func() error { close(entered); <-release; closed.Store(true); return nil }}
	reader := packageReader(t, meta, body)
	ports := &packagePorts{testPorts: &testPorts{}, open: func(context.Context, id.Actor, id.ProjectID, sc.SkillID, f.Revision) (*sc.PackageReader, error) {
		return reader, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newTestWriter()
	done := make(chan bool, 1)
	go func() {
		done <- serveTest(&handler{ports, &testBoundary{}}, httptest.NewRequest("GET", packagePath(), nil).WithContext(ctx), w)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("did not reach original Close")
	}
	select {
	case <-done:
		close(release)
		t.Fatal("handler returned before original Close")
	default:
	}
	cancel()
	close(release)
	select {
	case aborted := <-done:
		if !aborted || !closed.Load() || !reader.Joined() || body.closes.Load() != 1 || w.Body.Len() != 0 {
			t.Fatal("cancellation published or skipped actual Close")
		}
	case <-time.After(time.Second):
		t.Fatal("original handler did not join")
	}
}
