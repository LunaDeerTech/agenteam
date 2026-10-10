package secret

import (
	"bytes"
	"fmt"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func projectVariableEnvelope(t *testing.T) envelope {
	t.Helper()
	project, err := f.ParseID[i.Project]("01900000-0000-7000-8000-000000000004")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(project)
	if err != nil {
		t.Fatal(err)
	}
	p := vectorEnvelope(t)
	nonce, _ := masterNonce(17)
	p, err = seal(testKeys(t), 1, nonce, scope, projectVariableReceiptOwner, p.ownerID, p.id, bytes.Repeat([]byte{0x71}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProjectVariableReceiptEnvelopeAADAndRewrap(t *testing.T) {
	p := projectVariableEnvelope(t)
	// Construct expected bytes from the published layout, not the encoder.
	const prefix = "6167656e7465616d2e7365637265742e646174612e763100" + "00000001" + "02" + "01900000000070008000000000000004"
	const suffix = "01900000000070008000000000000001" + "01900000000070008000000000000002"
	for _, kind := range []ownerKind{valueOwner, receiptOwner, projectVariableReceiptOwner} {
		candidate := p
		candidate.ownerKind = kind
		aad, err := dataAAD(candidate)
		want := unhex(t, prefix+fmt.Sprintf("%02x", byte(kind))+suffix)
		if err != nil || !bytes.Equal(aad, want) {
			t.Fatal("owner AAD format changed")
		}
	}
	if len(p.ciphertext) != 48 {
		t.Fatal("kind3 digest ciphertext length")
	}
	plain, err := openEnvelope(testKeys(t), p)
	if err != nil || !bytes.Equal(plain, bytes.Repeat([]byte{0x71}, 32)) {
		t.Fatal("kind3 digest did not open")
	}
	clear(plain)
	nonce, _ := masterNonce(18)
	r, err := rewrap(testKeys(t), p, 2, nonce)
	if err != nil || r.ownerKind != projectVariableReceiptOwner || !bytes.Equal(r.ciphertext, p.ciphertext) || r.wrapRevision != 2 {
		t.Fatal("kind3 rewrap changed payload")
	}
	plain, err = openEnvelope(testKeys(t), r)
	if err != nil || !bytes.Equal(plain, bytes.Repeat([]byte{0x71}, 32)) {
		t.Fatal("rewrapped kind3 did not open")
	}
	clear(plain)
}

func TestProjectVariableReceiptIndependentKind2And3Vectors(t *testing.T) {
	// Python cryptography AESGCM generated these vectors independently: public
	// fixture master bytes 32..63, DEK 64..95, data nonce 0..11, plaintext q*32.
	// Domain/version/scope/IDs use the documented binary AAD layout above.
	for _, fixture := range []struct {
		kind            ownerKind
		cipher, wrapped string
	}{
		{receiptOwner, "4b741d0906fd35d1b363f1b3c99cbdcaf2b55c781d3ddc229f5199e730c0182f274245e1dfc8832996f8fb06f5f50a00", "030cc3e319d73fb7704c6c64e10b90303850311712e474487c7e4856ba7e4bafe8dcf8414eb2b8eedb1e88989225ebda"},
		{projectVariableReceiptOwner, "4b741d0906fd35d1b363f1b3c99cbdcaf2b55c781d3ddc229f5199e730c0182fe2b02684ac5816f9a70778aa62114a4b", "1becd5e33fe19021e865c580deb1856548773841990e120fdf6f7d41e5fb24315e187259d2c09e7528faae6b1f2ebca5"},
	} {
		t.Run(fmt.Sprint(fixture.kind), func(t *testing.T) {
			p := vectorEnvelope(t)
			project, _ := f.ParseID[i.Project]("01900000-0000-7000-8000-000000000004")
			p.scope, _ = i.InProject(project)
			p.ownerKind = fixture.kind
			p.wrapNonce, _ = masterNonce(uint64(fixture.kind))
			p.ciphertext = unhex(t, fixture.cipher)
			p.wrappedDEK = unhex(t, fixture.wrapped)
			plain, err := openEnvelope(testKeys(t), p)
			if err != nil || !bytes.Equal(plain, bytes.Repeat([]byte{'q'}, 32)) {
				t.Fatal("independent digest vector did not open")
			}
			clear(plain)
		})
	}
}

func TestProjectVariableReceiptEnvelopeStrictScopeAndLength(t *testing.T) {
	p := projectVariableEnvelope(t)
	for _, size := range []int{0, 1, 31, 33, 65536} {
		t.Run(fmt.Sprintf("plain/%d", size), func(t *testing.T) {
			if _, err := seal(testKeys(t), 1, p.wrapNonce, p.scope, projectVariableReceiptOwner, p.ownerID, p.id, make([]byte, size)); err == nil {
				t.Fatal("wrong digest size accepted")
			}
		})
	}
	if _, err := seal(testKeys(t), 1, p.wrapNonce, i.SystemScope(), projectVariableReceiptOwner, p.ownerID, p.id, make([]byte, 32)); err == nil {
		t.Fatal("kind3 System accepted")
	}
	for _, size := range []int{17, 47, 49, 65552} {
		q := p
		q.ciphertext = make([]byte, size)
		if _, err := wrapAAD(q); err == nil {
			t.Fatal("wrong kind3 ciphertext size accepted")
		}
	}
	for name, change := range map[string]func(*envelope){
		"kind1":        func(q *envelope) { q.ownerKind = valueOwner },
		"kind2":        func(q *envelope) { q.ownerKind = receiptOwner },
		"unknown-kind": func(q *envelope) { q.ownerKind = 4 },
		"system":       func(q *envelope) { q.scope = i.SystemScope() },
		"project": func(q *envelope) {
			id, _ := f.ParseID[i.Project]("01900000-0000-7000-8000-000000000005")
			q.scope, _ = i.InProject(id)
		},
		"receipt": func(q *envelope) { q.ownerID = "01900000-0000-7000-8000-000000000006" },
		"payload": func(q *envelope) { q.id, _ = f.ParseID[payloadMarker]("01900000-0000-7000-8000-000000000007") },
	} {
		t.Run(name, func(t *testing.T) {
			q := p
			change(&q)
			if value, err := openEnvelope(testKeys(t), q); err == nil || len(value) != 0 {
				t.Fatal("identity substitution accepted")
			}
		})
	}
}

type projectVariablePayloadRow struct {
	p    envelope
	kind int16
}

func (r projectVariablePayloadRow) Scan(dst ...any) error {
	p := r.p
	*dst[0].(*string) = p.id.String()
	*dst[1].(*string) = string(p.scope.Details().Kind)
	*dst[2].(*string) = p.scope.Details().ProjectID
	*dst[3].(*int16) = r.kind
	*dst[4].(*string) = p.ownerID
	*dst[5].(*int) = p.format
	*dst[6].(*string) = p.algorithm
	*dst[7].(*[]byte) = p.dataNonce
	*dst[8].(*[]byte) = p.ciphertext
	*dst[9].(*[]byte) = p.wrapNonce
	*dst[10].(*[]byte) = p.wrappedDEK
	*dst[11].(*int64) = int64(p.masterVersion)
	*dst[12].(*int64) = int64(p.wrapRevision)
	return nil
}
func TestProjectVariableReceiptScanRejectsWrappedKindsAndBounds(t *testing.T) {
	p := projectVariableEnvelope(t)
	if _, err := scanPayload(projectVariablePayloadRow{p, 3}); err != nil {
		t.Fatal("real kind3 shape rejected")
	}
	for _, kind := range []int16{-253, -1, 0, 4, 257, 258, 259, 32767} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			if _, err := scanPayload(projectVariablePayloadRow{p, kind}); err == nil {
				t.Fatal("out-of-domain kind wrapped into valid owner")
			}
		})
	}
	q := p
	q.scope = i.SystemScope()
	if _, err := scanPayload(projectVariablePayloadRow{q, 3}); err == nil {
		t.Fatal("kind3 System scanned")
	}
	q = p
	q.ciphertext = q.ciphertext[:47]
	if _, err := scanPayload(projectVariablePayloadRow{q, 3}); err == nil {
		t.Fatal("short kind3 digest scanned")
	}
}
