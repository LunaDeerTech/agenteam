package object

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// The canonical payload and final MAC were independently generated with Python
// json.dumps(sort_keys=True,separators=(',',':')) and hmac/sha256, key 06*32.
const downloadKnownPayload = `{"business_ref":{"artifact_id":"018bcfe5-6800-7000-8000-000000000004","file_id":"018bcfe5-6800-7000-8000-000000000005","kind":"artifact_file","project_id":"018bcfe5-6800-7000-8000-000000000003"},"byte_size":"3","expires_at":"2026-10-04T00:01:00.000000Z","filename":"report.txt","grant_id":"018bcfe5-6800-7000-8000-000000000001","media_type":"text/plain","method":"GET","mode":"preview","object_created_at":"2026-10-04T00:00:00.000000Z","object_id":"018bcfe5-6800-7000-8000-000000000006","object_version":"1","owner_id":"018bcfe5-6800-7000-8000-000000000004","owner_kind":"artifact","project_id":"018bcfe5-6800-7000-8000-000000000003","revision":"9007199254740993","sha256":"sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad","user_id":"018bcfe5-6800-7000-8000-000000000002"}`
const downloadKnownMAC = "NOVjrth6LSqk4V-UoZYlg4RnquV1aJ3SkuVuN_BCOYQ"

func downloadVectorClaims(t *testing.T) downloadClaims {
	t.Helper()
	var wire downloadWire
	if e := json.Unmarshal([]byte(downloadKnownPayload), &wire); e != nil {
		t.Fatal(e)
	}
	c, e := wire.claims()
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestDownloadTokenIndependentVectorAndEveryByte(t *testing.T) {
	k := downloadTestKeys(t)
	c := downloadVectorClaims(t)
	token, e := k.signDownload(c)
	if e != nil {
		t.Fatal(e)
	}
	wanted := "eyJraWQiOiJuZXciLCJ2IjoxfQ." + base64.RawURLEncoding.EncodeToString([]byte(downloadKnownPayload)) + "." + downloadKnownMAC
	if token != wanted {
		t.Fatal("download canonical/MAC vector mismatch")
	}
	got, e := k.verifyDownload(token)
	if e != nil {
		t.Fatal(e)
	}
	w, _ := got.wire()
	b, _ := canonicalDownload(w)
	if string(b) != downloadKnownPayload {
		t.Fatal("binding changed")
	}
	for i := range len(token) {
		mutated := []byte(token)
		mutated[i] ^= 1
		if _, e = k.verifyDownload(string(mutated)); e == nil {
			t.Fatalf("tamper at %d accepted", i)
		}
	}
	for _, bad := range []string{"", token + "=", strings.Replace(token, ".", "=.", 1), token + ".extra", strings.Repeat("a", downloadTokenLimit+1)} {
		if _, e = k.verifyDownload(bad); e == nil {
			t.Fatal("noncanonical token accepted")
		}
	}
}
func TestDownloadTokenRotationAndStrictSignedPayload(t *testing.T) {
	c, s := downloadTestInputs(t)
	old, e := LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"old","keys":[{"kid":"old","key_b64":%q}]}`, downloadTestMaterial(5)), c, s)
	if e != nil {
		t.Fatal(e)
	}
	token, e := old.signDownload(downloadVectorClaims(t))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = downloadTestKeys(t).verifyDownload(token); e != nil {
		t.Fatal("retained kid failed")
	}
	removed, e := LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"new","keys":[{"kid":"new","key_b64":%q}]}`, downloadTestMaterial(6)), c, s)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = removed.verifyDownload(token); e == nil {
		t.Fatal("removed kid accepted")
	}
	signRaw := func(header, payload string) string {
		hp := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." + base64.RawURLEncoding.EncodeToString([]byte(payload))
		key, _ := removed.key("new")
		h := hmac.New(sha256.New, key[:])
		h.Write([]byte("agenteam.object.download.v1\x00"))
		h.Write([]byte(hp))
		return hp + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	}
	for _, p := range []string{
		" " + downloadKnownPayload,
		strings.Replace(downloadKnownPayload, `"revision":"9007199254740993"`, `"revision":9007199254740993`, 1),
		strings.Replace(downloadKnownPayload, `"revision":"9007199254740993"`, `"revision":"09007199254740993"`, 1),
		strings.Replace(downloadKnownPayload, `"method":"GET"`, `"method":"POST"`, 1),
		strings.Replace(downloadKnownPayload, `"mode":"preview"`, `"mode":null`, 1),
		strings.Replace(downloadKnownPayload, `"filename":"report.txt"`, `"filename":"../report.txt"`, 1),
		strings.Replace(downloadKnownPayload, `"filename":"report.txt"`, `"filename":"report\r\n.txt"`, 1),
		strings.Replace(downloadKnownPayload, `"object_id":`, `"Object_id":`, 1),
		strings.Replace(downloadKnownPayload, `"byte_size":"3"`, `"byte_size":"3","byte_size":"3"`, 1),
		strings.Replace(downloadKnownPayload, `"method":"GET"`, `"extra":1,"method":"GET"`, 1),
	} {
		if _, e := removed.verifyDownload(signRaw(`{"kid":"new","v":1}`, p)); e == nil {
			t.Fatal("signed malformed payload accepted")
		}
	}
	for _, h := range []string{`{"kid":"new","v":2}`, `{"kid":"new","v":1,"v":1}`, `{"v":1,"kid":"new"}`, `{"kid":"new","v":1,"x":0}`} {
		if _, e := removed.verifyDownload(signRaw(h, downloadKnownPayload)); e == nil {
			t.Fatal("signed malformed header accepted")
		}
	}
}
func TestDownloadReferenceVariantRoundTrips(t *testing.T) {
	id := func(n int) string { return fmt.Sprintf("018bcfe5-6800-7000-8000-%012x", n) }
	project, _ := foundation.ParseID[identity.Project](id(1))
	objectID, _ := foundation.ParseID[oc.StoredObject](id(2))
	owner, _ := oc.NewObjectOwner(oc.Artifact, id(3), id(1))
	receiptID, _ := foundation.ParseID[oc.Receipt](id(4))
	uploadID, _ := foundation.ParseID[oc.Upload](id(5))
	receipt, e := oc.NewUploadReceipt(oc.ReceiptDetails{ID: receiptID, UploadID: uploadID, ObjectID: objectID, Owner: owner, CreationCause: id(6)})
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range []oc.BusinessFileDetails{
		{Kind: oc.UploadedObject, Receipt: receipt},
		{Kind: oc.ArtifactFile, ProjectID: project, ArtifactID: id(3), FileID: id(7)},
		{Kind: oc.KnowledgeFile, ProjectID: project, DocumentID: id(8), Revision: 9223372036854775807},
		{Kind: oc.ExecutionFile, ProjectID: project, ExecutionID: id(9), PayloadID: id(10)},
	} {
		r, e := oc.NewBusinessFileRef(d)
		if e != nil {
			t.Fatal(e)
		}
		w := encodeDownloadRef(r)
		got, e := decodeDownloadRef(w)
		if e != nil {
			t.Fatal(e)
		}
		if encodeDownloadRef(got) != w {
			t.Fatal("reference changed")
		}
	}
}
