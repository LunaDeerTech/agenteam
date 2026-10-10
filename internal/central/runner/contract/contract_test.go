package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func testID[K any](t *testing.T) f.ID[K] {
	t.Helper()
	v, e := f.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func human(t *testing.T, u id.UserID) id.Actor {
	t.Helper()
	a, e := id.NewHuman(u, testID[id.Session](t))
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestOriginalIntentOwnsPresenceAndIdentity(t *testing.T) {
	target := testID[Runner](t)
	u := testID[id.User](t)
	a := human(t, u)
	name := "original-canary"
	tags := []string{"a", "b"}
	v, e := NewUpdate(target, UpdateRequest{ExpectedVersion: 2, Name: &name, Tags: &tags})
	if e != nil {
		t.Fatal(e)
	}
	d, e := v.Digest(a)
	if e != nil {
		t.Fatal(e)
	}
	name = "changed"
	tags[0] = "foreign"
	original, _ := v.RequestJSON()
	if !strings.Contains(string(original), "original-canary") || strings.Contains(string(original), "foreign") {
		t.Fatal("caller changed intent")
	}
	original[0] = 'x'
	same, _ := v.Digest(human(t, u))
	if d != same {
		t.Fatal("same user new session changed identity")
	}
	other, _ := v.Digest(human(t, testID[id.User](t)))
	if other == d {
		t.Fatal("foreign user not bound")
	}
	empty := ""
	one, _ := NewUpdate(target, UpdateRequest{ExpectedVersion: 2, Description: &empty})
	n := "n"
	two, _ := NewUpdate(target, UpdateRequest{ExpectedVersion: 2, Name: &n, Description: &empty})
	x, _ := one.Digest(a)
	y, _ := two.Digest(a)
	if x == y {
		t.Fatal("patch presence lost")
	}
	key1, _ := v.Identity(a, "key/1")
	key2, _ := v.Identity(a, "key/2")
	if key1.Canonical() == key2.Canonical() || key1.Namespace() != "runner-management" || len(key1.OwnerIDs()) != 2 || key1.OwnerIDs()[0] != u.String() {
		t.Fatal("identity")
	}
	for _, s := range []string{fmt.Sprintf("%#v", v), fmt.Sprintf("%+v", struct{ V Intent }{v}), v.LogValue().String()} {
		if strings.Contains(s, "canary") {
			t.Fatal("implicit leak")
		}
	}
	b, _ := json.Marshal(v)
	if string(b) != `"runner_intent"` {
		t.Fatal("implicit intent projection")
	}
}
func TestManagementClosedWireAndAtomicDecode(t *testing.T) {
	target := testID[Runner](t)
	good := `{"runner_id":"` + target.String() + `","name":"valid","description":"","tags":[],"root_path":"/srv"}`
	var keep CreateRequest
	if e := json.Unmarshal([]byte(good), &keep); e != nil {
		t.Fatal(e)
	}
	tests := []string{
		strings.Replace(good, `"name":"valid"`, `"name":"valid","na\u006de":"other"`, 1),
		strings.Replace(good, `"description":""`, `"description":null`, 1),
		strings.Replace(good, `"description":""`, `"description":"\ud800"`, 1),
		strings.Replace(good, `"description":""`, `"Description":""`, 1),
		strings.Replace(good, `"tags":[]`, `"tags":["b","a"]`, 1),
		strings.Replace(good, `"tags":[]`, `"tags":null`, 1),
		strings.Replace(good, `"root_path":"/srv"`, `"root_path":"/srv/../x"`, 1),
		good + ` {}`,
	}
	for n, raw := range tests {
		v := keep
		if e := json.Unmarshal([]byte(raw), &v); e == nil {
			t.Fatalf("invalid case %d accepted: %.80s", n, raw)
		}
		if v.Name != keep.Name || v.RunnerID != keep.RunnerID {
			t.Fatal("partial decode")
		}
	}
	for _, raw := range []string{`{"expected_version":"2"}`, `{"expected_version":"2","name":null}`, `{"expected_version":2,"name":"x"}`, `{"expected_version":"02","name":"x"}`, `{"expected_version":"2","tags":[null]}`, `{"expected_version":"2","tags":[""]}`} {
		var v UpdateRequest
		if json.Unmarshal([]byte(raw), &v) == nil {
			t.Fatal("bad patch accepted")
		}
	}
	if _, e := DecodeIntent(Create, testID[Runner](t), []byte(good)); e == nil {
		t.Fatal("cross target")
	}
	if _, e := DecodeIntent(Create, target, []byte(good+strings.Repeat(" ", MaxRequestBytes))); e == nil {
		t.Fatal("raw request cap")
	}
	escaped := strings.Replace(good, `"description":""`, `"description":"\ud83d\ude03"`, 1)
	var positive CreateRequest
	if e := json.Unmarshal([]byte(escaped), &positive); e != nil {
		t.Fatal("valid surrogate pair rejected", e)
	}
}
func TestPublicReceiptAndHelloRoundTrip(t *testing.T) {
	target := testID[Runner](t)
	at, _ := f.NewInstant(time.Now())
	fp := f.Digest("sha256:" + strings.Repeat("a", 64))
	h := HelloSnapshot{RunnerID: p.ID(target.String()), RunnerVersion: "test", ProtocolVersion: p.Version{Major: 1, Minor: 0}, OS: "linux", Arch: "amd64", Headless: true, Capabilities: []string{}, FeatureFlags: []string{}}
	s := Snapshot{ID: target, Name: "runner", Tags: []string{}, RootPath: "/srv", Version: 2, CredentialGeneration: 1, Status: Online, PublicKeyFingerprint: &fp, EnrolledAt: &at, LastSeenAt: &at, LastHello: &h, CreatedAt: at, UpdatedAt: at}
	r := Receipt{CommandID: testID[Command](t), Command: Update, Runner: s, Changed: true}
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "runner_payload") {
		t.Fatal("redaction replaced explicit hello projection")
	}
	var round Receipt
	if e = json.Unmarshal(b, &round); e != nil {
		t.Fatal(e)
	}
	if round.Runner.LastHello.OS != "linux" {
		t.Fatal("hello lost")
	}
	copy := r.Clone()
	copy.Runner.LastHello.Capabilities = append(copy.Runner.LastHello.Capabilities, "other")
	if len(r.Runner.LastHello.Capabilities) != 0 {
		t.Fatal("clone alias")
	}
	unknown := strings.Replace(string(b), `"headless":true`, `"headless":true,"private_seed":"canary"`, 1)
	if json.Unmarshal([]byte(unknown), &round) == nil {
		t.Fatal("nested unknown")
	}
	token, e := p.NewEnrollmentToken()
	if e != nil {
		t.Fatal(e)
	}
	material := EnrollmentMaterial{token, at}
	secret, _ := token.Wire()
	encoded, _ := json.Marshal(struct{ M EnrollmentMaterial }{material})
	if strings.Contains(string(encoded), secret) || strings.Contains(fmt.Sprintf("%#v", material), secret) {
		t.Fatal("material leak")
	}
	s.PublicKeyFingerprint = nil
	if s.Validate() == nil {
		t.Fatal("online missing key")
	}
}
