//go:build integration

package skill_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

const skillPackageHTTPEntry = "---\nname: Package HTTP guide\ndescription: Real Owner package installation\n---\n原始说明。\n"

func skillPackageHTTPInstall(t *testing.T, v *skillPackageHTTPFixture) sc.Metadata {
	t.Helper()
	target := testID[pc.Skill](t)
	body, err := json.Marshal(map[string]any{"request": map[string]any{
		"skill_id": target.String(), "mode": "create",
		"source": map[string]any{"kind": "text_files", "files": []map[string]string{
			{"path": "SKILL.md", "utf8_text": skillPackageHTTPEntry},
			{"path": "references/guide.txt", "utf8_text": "Canonical reference 世界\n"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	response := v.request(t, v.ownerBrowser, http.MethodPost, skillOwnerHTTPPath(v.project, ""), string(body), f.IdempotencyKey(testID[struct{}](t).String()))
	receipt := response.want(t, http.StatusOK)
	if len(receipt) != 3 || receipt["skill_id"] != target.String() || receipt["revision"] != "1" || receipt["version"] != "1" {
		t.Fatal("HTTP installation did not return its original revision")
	}
	return skillPackageHTTPMetadata(t, v, target)
}

func skillPackageHTTPMetadata(t *testing.T, v *skillPackageHTTPFixture, target sc.SkillID) sc.Metadata {
	t.Helper()
	response := v.request(t, v.ownerBrowser, http.MethodGet, skillOwnerHTTPPath(v.project, "/"+target.String()), "", "")
	response.want(t, http.StatusOK)
	var metadata sc.Metadata
	if json.Unmarshal(response.body, &metadata) != nil || metadata.Validate() != nil || metadata.ID != target || metadata.ProjectID != v.project || metadata.CurrentRevision != 1 {
		t.Fatal("original metadata GET did not expose the published revision")
	}
	return metadata
}

func skillPackageHTTPPath(v *skillPackageHTTPFixture, metadata sc.Metadata) string {
	return skillOwnerHTTPPath(v.project, "/"+metadata.ID.String()+"/revisions/"+strconv.FormatInt(int64(metadata.CurrentRevision), 10)+"/package")
}

func skillPackageHTTPWork(t *testing.T, v *skillPackageHTTPFixture, target sc.SkillID) int {
	t.Helper()
	var count, unjoined, leases int
	err := v.store.QueryRow(testContext(t), `SELECT
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND skill_id=$2 AND kind IN ('package_reader','installed_package_reader')),
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND skill_id=$2 AND phase<>'joined'),
 (SELECT count(*) FROM agenteam_object.object_leases l JOIN agenteam_skill.revisions r ON r.object_id=l.object_id WHERE r.project_id=$1 AND r.skill_id=$2 AND l.owner_kind='reader' AND l.state='active')`, v.project.String(), target.String()).Scan(&count, &unjoined, &leases)
	if err != nil || unjoined != 0 || leases != 0 {
		t.Fatal("original Skill/Object readers did not retire", err)
	}
	return count
}

// Reader work and released Object leases are real bookkeeping, not mutations
// of immutable revisions, installations, canonical objects, Audit or Activity.
func skillPackageHTTPFacts(t *testing.T, v *skillPackageHTTPFixture) [7]string {
	t.Helper()
	var facts [7]string
	for i, table := range []string{"agenteam_skill.installations", "agenteam_skill.skills", "agenteam_skill.revisions", "agenteam_object.objects", "agenteam_audit.audit_records", "agenteam_outbox.events"} {
		if err := v.store.QueryRow(testContext(t), `SELECT md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) FROM `+table+` x WHERE project_id=$1`, v.project.String()).Scan(&facts[i]); err != nil {
			t.Fatal("immutable package facts snapshot", err)
		}
	}
	if err := v.store.QueryRow(testContext(t), `SELECT md5(COALESCE(jsonb_agg(jsonb_build_array(id::text,last_activity_at) ORDER BY id)::text,'[]')) FROM agenteam_account.sessions`).Scan(&facts[6]); err != nil {
		t.Fatal("Session Activity snapshot", err)
	}
	return facts
}

func skillPackageHTTPArchive(t *testing.T, v *skillPackageHTTPFixture, metadata sc.Metadata, raw []byte, files map[string]string) {
	t.Helper()
	var digest, manifest string
	var size int64
	err := v.store.QueryRow(testContext(t), `SELECT 'sha256:'||encode(o.sha256,'hex'),o.byte_size,COALESCE(i.manifest::text,convert_from(n.manifest,'UTF8'))
 FROM agenteam_skill.revisions r JOIN agenteam_object.objects o ON o.id=r.object_id
 LEFT JOIN agenteam_skill.initializations i ON i.revision_id=r.id
 LEFT JOIN agenteam_skill.installations n ON n.id=r.installation_id
 WHERE r.project_id=$1 AND r.skill_id=$2 AND r.revision=$3`, v.project.String(), metadata.ID.String(), int64(metadata.CurrentRevision)).Scan(&digest, &size, &manifest)
	if err != nil || size != int64(len(raw)) || digest != fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) {
		t.Fatal("HTTP archive differs from the immutable canonical Object", err)
	}
	stored, err := sc.DecodeManifest([]byte(manifest))
	if err != nil {
		t.Fatal("original manifest could not be decoded")
	}
	details, err := stored.Details()
	if err != nil || details.EntryPath != sc.EntryPath {
		t.Fatal("original package manifest invalid", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(archive.File) != len(details.Files) {
		t.Fatal("HTTP package ZIP/manifest differs", err)
	}
	seen := map[string]bool{}
	for index, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal("ZIP entry could not be opened")
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, sc.MaxFileBytes+1))
		closeErr := reader.Close()
		entry := details.Files[index]
		if readErr != nil || closeErr != nil || seen[file.Name] || file.Name != entry.Path || len(body) != int(entry.ByteSize) || entry.SHA256.String() != fmt.Sprintf("sha256:%x", sha256.Sum256(body)) {
			t.Fatal("ZIP entry differs from immutable manifest")
		}
		if files != nil && files[file.Name] != string(body) {
			t.Fatal("ZIP entry differs from original HTTP text_files")
		}
		seen[file.Name] = true
	}
	if !seen[sc.EntryPath] || files != nil && len(seen) != len(files) {
		t.Fatal("HTTP package lost or added an entry")
	}
}

func skillPackageHTTPHeaders(t *testing.T, response skillOwnerHTTPResponse, length int, head bool) {
	t.Helper()
	if response.aborted || response.status != http.StatusOK || response.header.Get("Content-Type") != sc.PackageMediaType || response.header.Get("Content-Disposition") != `attachment; filename="skill-package.zip"` || response.header.Get("Cache-Control") != "no-store" || response.header.Get("X-Content-Type-Options") != "nosniff" || response.header.Get("Content-Length") != strconv.Itoa(length) || len(response.header.Values("X-Request-ID")) != 1 {
		t.Fatal("package response lost exact safe representation headers")
	}
	if _, err := f.ParseID[f.Request](response.header.Get("X-Request-ID")); err != nil {
		t.Fatal("package request identity invalid")
	}
	if head && len(response.body) != 0 || !head && len(response.body) != length {
		t.Fatal("package response body length disagrees with method")
	}
}

func TestSkillOwnerPackageHTTPPublication(t *testing.T) {
	skillOwnerHTTPTop(t)
	v := newSkillPackageHTTPFixture(t)
	installed := skillPackageHTTPInstall(t, v)
	listing, err := v.service.ListSkills(testContext(t), v.ownerBrowser.actor, v.project)
	if err != nil || len(listing) != 1 || !listing[0].Protected {
		t.Fatal("real builtin publication missing", err)
	}
	for _, item := range []struct {
		name     string
		metadata sc.Metadata
		files    map[string]string
	}{
		{"builtin", skillPackageHTTPMetadata(t, v, listing[0].ID), nil},
		{"installed", installed, map[string]string{sc.EntryPath: skillPackageHTTPEntry, "references/guide.txt": "Canonical reference 世界\n"}},
	} {
		t.Run(item.name, func(t *testing.T) {
			before := skillPackageHTTPFacts(t, v)
			work := skillPackageHTTPWork(t, v, item.metadata.ID)
			path := skillPackageHTTPPath(v, item.metadata)
			get := v.request(t, v.ownerBrowser, http.MethodGet, path, "", "")
			skillPackageHTTPHeaders(t, get, len(get.body), false)
			skillPackageHTTPArchive(t, v, item.metadata, get.body, item.files)
			if item.metadata.Protected && fmt.Sprintf("sha256:%x", sha256.Sum256(get.body)) != skill.AddSkillsPackageSHA256.String() {
				t.Fatal("builtin canonical package digest changed")
			}
			if skillPackageHTTPWork(t, v, item.metadata.ID) != work+1 {
				t.Fatal("GET did not retain one actual closed package-reader work")
			}
			head := v.request(t, v.ownerBrowser, http.MethodHead, path, "", "")
			skillPackageHTTPHeaders(t, head, len(get.body), true)
			if skillPackageHTTPWork(t, v, item.metadata.ID) != work+2 || skillPackageHTTPFacts(t, v) != before {
				t.Fatal("HEAD skipped its original read/Close or read mutated immutable business facts")
			}
		})
	}
}

func TestSkillOwnerPackageHTTPCurrentAuthority(t *testing.T) {
	skillOwnerHTTPTop(t)
	v := newSkillPackageHTTPFixture(t)
	metadata := skillPackageHTTPInstall(t, v)
	path := skillPackageHTTPPath(v, metadata)
	before := skillPackageHTTPFacts(t, v)
	work := skillPackageHTTPWork(t, v, metadata.ID)
	for _, browser := range []skillOwnerHTTPBrowser{v.otherBrowser, v.adminBrowser} {
		v.request(t, browser, http.MethodGet, path, "", "").want(t, http.StatusNotFound)
		head := v.request(t, browser, http.MethodHead, path, "", "")
		if head.aborted || head.status != http.StatusNotFound || len(head.body) != 0 {
			t.Fatal("unauthorized HEAD returned package bytes")
		}
	}
	missing := skillOwnerHTTPPath(v.project, "/"+metadata.ID.String()+"/revisions/2/package")
	v.request(t, v.ownerBrowser, http.MethodGet, missing, "", "").want(t, http.StatusNotFound)
	if skillPackageHTTPWork(t, v, metadata.ID) != work || skillPackageHTTPFacts(t, v) != before {
		t.Fatal("denied package read performed external work or changed facts")
	}
	if err := v.core.Logout(testContext(t), account.LogoutRequest{Actor: v.ownerBrowser.actor, Key: f.IdempotencyKey(testID[struct{}](t).String())}); err != nil {
		t.Fatal("formal Logout", err)
	}
	before = skillPackageHTTPFacts(t, v)
	problem := v.request(t, v.ownerBrowser, http.MethodGet, path, "", "").want(t, http.StatusUnauthorized)
	if problem["code"] != string(f.SessionRevoked) {
		t.Fatal("revoked package request lost original Session rejection")
	}
	head := v.request(t, v.ownerBrowser, http.MethodHead, path, "", "")
	if head.aborted || head.status != http.StatusUnauthorized || len(head.body) != 0 || skillPackageHTTPWork(t, v, metadata.ID) != work || skillPackageHTTPFacts(t, v) != before {
		t.Fatal("revoked package HEAD escaped current Session gate")
	}
}
