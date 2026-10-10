package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func installTestID[K any](t *testing.T) f.ID[K] {
	t.Helper()
	x, err := f.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return x
}
func installTestPackage(t *testing.T, name, body string) Package {
	t.Helper()
	files, err := sc.NewTextFiles([]sc.TextFile{{Path: sc.EntryPath, UTF8Text: "---\nname: " + name + "\ndescription: Install fixture\n---\n" + body}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := BuildPackage(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInstallRequestUsesCanonicalPackageAndProtectsBuiltin(t *testing.T) {
	p := installTestPackage(t, "Release guide", "private install text")
	skill := installTestID[pc.Skill](t)
	r, err := NewInstallRequest(context.Background(), skill, p)
	if err != nil || r.Validate() != nil || r.SkillID() != skill {
		t.Fatal("valid package rejected", err)
	}
	body, _ := p.Bytes()
	parsed, err := ParseCanonicalPackage(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := NewInstallRequest(context.Background(), skill, parsed)
	if err != nil || r.data().packageDigest != copy.data().packageDigest || r.data().manifestDigest != copy.data().manifestDigest {
		t.Fatal("real package roundtrip changed input", err)
	}
	body[0] ^= 255
	if r.Validate() != nil {
		t.Fatal("caller's byte copy changed request")
	}
}

func TestInstallRequestRejectsUnboundAndCanceledInputs(t *testing.T) {
	p := installTestPackage(t, "Release guide", "ordinary package")
	skill := installTestID[pc.Skill](t)
	for _, tc := range []struct {
		ctx   context.Context
		skill sc.SkillID
		pkg   Package
	}{
		{nil, skill, p}, {context.Background(), sc.SkillID{}, p}, {context.Background(), skill, Package{}},
	} {
		if _, err := NewInstallRequest(tc.ctx, tc.skill, tc.pkg); err == nil {
			t.Fatal("invalid installation value accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewInstallRequest(ctx, skill, p); !errors.Is(err, context.Canceled) {
		t.Fatal("original cancellation lost", err)
	}
	for _, name := range []string{"Add Skills", "ADD SKILLS", "add-skills"} {
		if _, err := NewInstallRequest(context.Background(), skill, installTestPackage(t, name, "reserved name")); err == nil {
			t.Fatal("ordinary install overwrites protected identity")
		}
	}
}

func TestInstallRequestSemanticAndDefaultLoggingAreSafe(t *testing.T) {
	const canary = "install-body-private-canary"
	p := installTestPackage(t, "Release guide", canary)
	skill := installTestID[pc.Skill](t)
	r, err := NewInstallRequest(context.Background(), skill, p)
	if err != nil {
		t.Fatal(err)
	}
	project, user := installTestID[id.Project](t), installTestID[id.User](t)
	want, err := installSemantic(project, user, r.data())
	if err != nil {
		t.Fatal(err)
	}
	if again, err := installSemantic(project, user, r.data()); err != nil || again != want {
		t.Fatal("same command content is unstable", err)
	}
	otherBody, _ := NewInstallRequest(context.Background(), skill, installTestPackage(t, "Release guide", "different content"))
	otherTarget, _ := NewInstallRequest(context.Background(), installTestID[pc.Skill](t), p)
	for _, tc := range []struct {
		project id.ProjectID
		user    id.UserID
		request InstallRequest
	}{
		{installTestID[id.Project](t), user, r}, {project, installTestID[id.User](t), r}, {project, user, otherBody}, {project, user, otherTarget},
	} {
		got, e := installSemantic(tc.project, tc.user, tc.request.data())
		if e != nil || got == want {
			t.Fatal("original content/owner binding lost", e)
		}
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("request", "direct", r, "nested", map[string]any{"requests": []InstallRequest{r}})
	raw, err := json.Marshal(struct{ Request InstallRequest }{r})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{logs.String(), string(raw), fmt.Sprintf("%+v", []InstallRequest{r})} {
		if strings.Contains(text, canary) {
			t.Fatal("default output contains package bytes")
		}
	}
}
