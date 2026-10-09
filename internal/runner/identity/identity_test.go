package identity

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func configuration() Configuration {
	return Configuration{"https://central.test", p.ID("01900000-0000-7000-8000-000000000001"), "/runner/work"}
}
func pending(t *testing.T) Identity {
	t.Helper()
	v, err := newPending(configuration(), bytes.NewReader(bytes.Repeat([]byte{37}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestIdentityStrictFileAndState(t *testing.T) {
	v := pending(t)
	raw, err := encodeFile(v)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeFile(raw)
	if err != nil || decoded != v {
		t.Fatalf("roundtrip: %v", err)
	}
	key, err := v.PublicKey()
	active, e := v.AsActive()
	activeKey, e2 := active.PublicKey()
	if err != nil || e != nil || e2 != nil || key != activeKey || active.State() != Active || v.State() != Pending {
		t.Fatal("activation changed identity")
	}
	valid := string(raw)
	for name, bad := range map[string]string{
		"extra":         strings.Replace(valid, `"version":1`, `"version":1,"other":1`, 1),
		"duplicate":     strings.Replace(valid, `"state":"pending"`, `"state":"pending","st\u0061te":"active"`, 1),
		"case":          strings.Replace(valid, `"root_path"`, `"Root_path"`, 1),
		"null":          strings.Replace(valid, `"state":"pending"`, `"state":null`, 1),
		"missing":       strings.Replace(valid, `"state":"pending",`, ``, 1),
		"tail":          valid + `{}`,
		"seed_padding":  strings.Replace(valid, `"private_seed":"`, `"private_seed":"=`, 1),
		"state":         strings.Replace(valid, `"pending"`, `"registered"`, 1),
		"version":       strings.Replace(valid, `"version":1`, `"version":1.0`, 1),
		"surrogate":     strings.Replace(valid, `/runner/work`, `/runner/\ud800`, 1),
		"low_surrogate": strings.Replace(valid, `/runner/work`, `/runner/\udc00`, 1),
		"too_large":     valid + strings.Repeat(" ", MaxFileBytes),
	} {
		t.Run(name, func(t *testing.T) {
			if value, e := decodeFile([]byte(bad)); e != ErrInvalid || value != (Identity{}) {
				t.Fatal("invalid file accepted")
			}
		})
	}
	if _, err := decodeFile([]byte(strings.Replace(valid, `/runner/work`, `\/runner/\ud83d\ude00`, 1))); err != nil {
		t.Fatal("valid escaped Unicode path rejected", err)
	}
	if _, err := newPending(configuration(), bytes.NewReader([]byte{1})); err != ErrUnavailable {
		t.Fatal("entropy failure accepted")
	}
}

func TestIdentitySafeDefaults(t *testing.T) {
	v := pending(t)
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("safe", "identity", v, "config", v.Configuration())
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	all := fmt.Sprintf("%#v %+v %s %q", v, v.Configuration(), encoded, v) + logs.String()
	for _, private := range []string{base64.RawURLEncoding.EncodeToString(v.seed[:]), v.config.RootPath, v.config.CentralURL, string(v.config.RunnerID)} {
		if strings.Contains(all, private) {
			t.Fatal("default output disclosed identity material")
		}
	}
	for _, bad := range []Configuration{
		{CentralURL: "http://central.test", RunnerID: v.config.RunnerID, RootPath: v.config.RootPath},
		{CentralURL: "https://secret@central.test", RunnerID: v.config.RunnerID, RootPath: v.config.RootPath},
		{CentralURL: "https://central.test/", RunnerID: v.config.RunnerID, RootPath: v.config.RootPath},
		{CentralURL: "https://central.test?", RunnerID: v.config.RunnerID, RootPath: v.config.RootPath},
		{CentralURL: v.config.CentralURL, RunnerID: v.config.RunnerID, RootPath: "/a/../b"},
		{CentralURL: v.config.CentralURL, RunnerID: v.config.RunnerID, RootPath: "relative"},
	} {
		if bad.Validate() != ErrInvalid {
			t.Fatal("invalid configuration accepted")
		}
	}
}
