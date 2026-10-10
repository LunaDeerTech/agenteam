package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

const installTestText = "---\nname: Example\ndescription: Test package\n---\nTask-local package canary.\n"

func installTestID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, err := f.NewID[T]()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func installTestArguments(t *testing.T, files []sc.TextFile) []byte {
	t.Helper()
	rows := make([]map[string]string, 0, len(files))
	for _, file := range files {
		rows = append(rows, map[string]string{"path": file.Path, "utf8_text": file.UTF8Text})
	}
	raw, err := json.Marshal(map[string]any{"mode": "create", "source": map[string]any{"kind": "text_files", "files": rows}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func installTestPackage(t *testing.T) SkillInstallPackage {
	t.Helper()
	pkg, err := ParseSkillInstall(context.Background(), installTestArguments(t, []sc.TextFile{{Path: "SKILL.md", UTF8Text: installTestText}}))
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func installTestCall(t *testing.T, pkg SkillInstallPackage) SkillInstallCall {
	t.Helper()
	actor, err := id.NewAgentRun(installTestID[id.Project](t), installTestID[id.Agent](t), installTestID[id.Execution](t))
	if err != nil {
		t.Fatal(err)
	}
	call, err := NewSkillInstallCall(context.Background(), actor, tc.SpecRef{ToolID: installTestID[id.Tool](t), SpecRevision: 3}, installTestID[struct{}](t).String(), installTestID[pc.Skill](t), pkg)
	if err != nil {
		t.Fatal(err)
	}
	return call
}

func installTestReceipt(t *testing.T, call SkillInstallCall) skill.InstallReceipt {
	t.Helper()
	d, err := call.Details()
	if err != nil {
		t.Fatal(err)
	}
	return skill.InstallReceipt{InstallationID: installTestID[skill.Installation](t), SkillID: d.SkillID, ProjectID: d.ProjectID,
		RevisionID: installTestID[sc.Revision](t), Revision: 1, Version: 1, ObjectID: installTestID[oc.StoredObject](t), PackageSHA256: d.PackageSHA256}
}

func TestSkillInstallParseCanonicalPackage(t *testing.T) {
	ctx := context.Background()
	files := []sc.TextFile{{Path: "empty.txt"}, {Path: "SKILL.md", UTF8Text: installTestText}, {Path: "notes.md", UTF8Text: "Emoji 😀 and literal \\uD800"}}
	parsed, err := ParseSkillInstall(ctx, installTestArguments(t, files))
	if err != nil {
		t.Fatal(err)
	}
	domain, err := sc.NewTextFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := skill.BuildPackage(ctx, domain)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := parsed.Package()
	x, _ := expected.Bytes()
	y, _ := actual.Bytes()
	if !bytes.Equal(x, y) {
		t.Fatal("adapter changed domain package bytes")
	}
	// Pair escapes retain exactly the same valid Unicode content.
	escaped := bytes.ReplaceAll(installTestArguments(t, files), []byte("😀"), []byte(`\ud83d\ude00`))
	paired, err := ParseSkillInstall(ctx, escaped)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := parsed.Facts()
	b, _ := paired.Facts()
	if a != b {
		t.Fatal("equivalent Unicode changed package")
	}
	y[0] ^= 1
	still, _ := actual.Bytes()
	if !bytes.Equal(x, still) {
		t.Fatal("caller mutated package")
	}
	definition := SkillInstallDefinition()
	if _, err = tc.CanonicalDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}
	definition.InputSchema[0] = '!'
	definition.Annotations.ReadOnly = true
	fresh := SkillInstallDefinition()
	if !json.Valid(fresh.InputSchema) || fresh.Annotations.ReadOnly || fresh.StableKey != "builtin:install-skill" {
		t.Fatal("definition alias or incorrect identity")
	}
}

func TestSkillInstallParseRejectsInvalidArguments(t *testing.T) {
	valid := installTestArguments(t, []sc.TextFile{{Path: "SKILL.md", UTF8Text: installTestText}})
	cases := map[string][]byte{
		"null": []byte(`null`), "extra-value": append(bytes.Clone(valid), []byte(` {}`)...),
		"missing-mode":           bytes.Replace(valid, []byte(`"mode":"create",`), nil, 1),
		"duplicate-mode":         bytes.Replace(valid, []byte(`"mode":"create"`), []byte(`"mode":"create","mode":"create"`), 1),
		"escaped-duplicate":      bytes.Replace(valid, []byte(`"mode":"create"`), []byte(`"mode":"create","\u006dode":"create"`), 1),
		"update":                 bytes.Replace(valid, []byte(`"create"`), []byte(`"update"`), 1),
		"unknown-top":            bytes.Replace(valid, []byte(`"mode":"create"`), []byte(`"mode":"create","project_id":"untrusted"`), 1),
		"runner":                 bytes.Replace(valid, []byte(`"text_files"`), []byte(`"runner_package"`), 1),
		"duplicate-source-field": bytes.Replace(valid, []byte(`"kind":"text_files"`), []byte(`"kind":"text_files","kind":"text_files"`), 1),
		"duplicate-file-field":   bytes.Replace(valid, []byte(`"path":"SKILL.md"`), []byte(`"path":"SKILL.md","path":"SKILL.md"`), 1),
		"unknown-file-field":     bytes.Replace(valid, []byte(`"path":"SKILL.md"`), []byte(`"path":"SKILL.md","url":"forbidden"`), 1),
		"null-text":              bytes.Replace(valid, mustJSONString(t, installTestText), []byte(`null`), 1),
		"surrogate-high":         bytes.Replace(valid, []byte("Example"), []byte(`\ud800`), 1),
		"surrogate-low":          bytes.Replace(valid, []byte("Example"), []byte(`\udfff`), 1),
		"surrogate-nonpair":      bytes.Replace(valid, []byte("Example"), []byte(`\ud800\u0041`), 1),
		"invalid-utf8":           bytes.Replace(valid, []byte("Example"), []byte{0xff}, 1),
		"wrong-nesting":          []byte(`{"mode":"create","source":{"kind":"text_files","files":[[]]}}`),
		"no-files":               installTestArguments(t, nil),
		"domain-path":            installTestArguments(t, []sc.TextFile{{Path: "SKILL.md", UTF8Text: installTestText}, {Path: "../bad", UTF8Text: "text"}}),
		"byte-path-limit":        installTestArguments(t, []sc.TextFile{{Path: "SKILL.md", UTF8Text: installTestText}, {Path: strings.Repeat("é", 513), UTF8Text: "text"}}),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			pkg, err := ParseSkillInstall(context.Background(), raw)
			if err == nil || pkg.Validate() == nil {
				t.Fatal("invalid input produced package")
			}
		})
	}
}

func mustJSONString(t *testing.T, s string) []byte {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSkillInstallLimitsCancellationAndSafeFormatting(t *testing.T) {
	raw := installTestArguments(t, []sc.TextFile{{Path: "SKILL.md", UTF8Text: installTestText}})
	exact := append(bytes.Clone(raw), bytes.Repeat([]byte(" "), MaxSkillInstallArguments-len(raw))...)
	if _, err := ParseSkillInstall(context.Background(), exact); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSkillInstall(context.Background(), append(exact, ' ')); err == nil {
		t.Fatal("oversize accepted")
	}
	files := make([]sc.TextFile, 129)
	files[0] = sc.TextFile{Path: "SKILL.md", UTF8Text: installTestText}
	for i := 1; i < len(files); i++ {
		files[i] = sc.TextFile{Path: fmt.Sprintf("file-%03d", i)}
	}
	if _, err := ParseSkillInstall(context.Background(), installTestArguments(t, files)); err == nil {
		t.Fatal("129 files accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseSkillInstall(ctx, raw); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
	if _, err := ParseSkillInstall(nil, raw); err == nil {
		t.Fatal("nil context accepted")
	}
	pkg := installTestPackage(t)
	encoded, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded)+fmt.Sprintf("%+v", pkg), "canary") {
		t.Fatal("package leaked")
	}
	if json.Unmarshal(raw, &pkg) == nil {
		t.Fatal("wire replaced package handle")
	}
}

func TestSkillInstallCallScopeAndReceipt(t *testing.T) {
	ctx := context.Background()
	pkg := installTestPackage(t)
	call := installTestCall(t, pkg)
	d, _ := call.Details()
	actor, _ := call.Actor()
	copyCall, err := NewSkillInstallCall(ctx, actor, d.Spec, d.OperationID, d.SkillID, pkg)
	if err != nil {
		t.Fatal(err)
	}
	copyDetails, _ := copyCall.Details()
	if copyDetails != d {
		t.Fatal("original call material changed")
	}
	d.SkillID = installTestID[pc.Skill](t)
	unchanged, _ := call.Details()
	if unchanged.SkillID == d.SkillID {
		t.Fatal("mutable details")
	}
	scope, err := call.CurrentScope(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rawScope, _ := json.Marshal(scope)
	if scope.Action != "skill.install.create" || scope.ProjectID != unchanged.ProjectID || bytes.Contains(rawScope, []byte("key")) {
		t.Fatal("wrong scope")
	}
	risks, err := call.Risks(ctx)
	if err != nil || risks == nil || len(risks) != 0 || call.ReusableScopeSupported() {
		t.Fatal("wrong risk/reusable projection")
	}
	r := installTestReceipt(t, call)
	out, err := ProjectSkillInstallReceipt(ctx, call, r)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	if json.Unmarshal(out, &result) != nil || len(result) != 3 || result["skill_id"] != r.SkillID.String() || result["revision"] != "1" || result["version"] != "1" {
		t.Fatal("wrong successful projection")
	}
	changes := []func(*skill.InstallReceipt){
		func(v *skill.InstallReceipt) { v.SkillID = installTestID[pc.Skill](t) },
		func(v *skill.InstallReceipt) { v.ProjectID = installTestID[id.Project](t) },
		func(v *skill.InstallReceipt) { v.PackageSHA256 = f.Digest("sha256:" + strings.Repeat("0", 64)) },
		func(v *skill.InstallReceipt) { v.Version = 2 }, func(v *skill.InstallReceipt) { v.Revision = 2 },
	}
	for _, change := range changes {
		bad := r
		change(&bad)
		out, err = ProjectSkillInstallReceipt(ctx, call, bad)
		if !errors.Is(err, ErrSkillInstallReceipt) || out != nil {
			t.Fatal("mismatched receipt exposed")
		}
	}
	human, _ := id.NewHuman(installTestID[id.User](t), installTestID[id.Session](t))
	if _, err = NewSkillInstallCall(ctx, human, unchanged.Spec, unchanged.OperationID, unchanged.SkillID, pkg); err == nil {
		t.Fatal("Human accepted as Tool actor")
	}
	if _, err = SkillInstallKey(strings.ToUpper(unchanged.OperationID)); err == nil && strings.ToUpper(unchanged.OperationID) != unchanged.OperationID {
		t.Fatal("noncanonical operation accepted")
	}
}
