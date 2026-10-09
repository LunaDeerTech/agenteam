package runnerprotocol_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestDeviceHTTPClosedWireAndAtomicDecode(t *testing.T) {
	nonce, err := p.NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	token, err := p.NewEnrollmentToken()
	if err != nil {
		t.Fatal(err)
	}
	var public [32]byte
	for i := range public {
		public[i] = byte(i + 1)
	}
	challengeRequest, _ := p.NewChallengeRequest(first)
	challengeResponse, _ := p.NewChallengeResponse(nonce, at)
	enrollRequest, _ := p.NewEnrollmentRequest(first, token, public, "/workspace/private", "linux", "amd64")
	enrollResponse, _ := p.NewEnrollmentResponse(first, "2", "1", at, p.PublicKeyFingerprint(public))
	requestRaw, _ := p.EncodeChallengeRequest(challengeRequest)
	responseRaw, _ := p.EncodeChallengeResponse(challengeResponse)
	enrollRaw, _ := p.EncodeEnrollmentRequest(enrollRequest)
	enrolledRaw, _ := p.EncodeEnrollmentResponse(enrollResponse)
	for _, example := range []struct {
		name string
		raw  []byte
		into json.Unmarshaler
	}{
		{"challenge-request", requestRaw, &challengeRequest},
		{"challenge-response", responseRaw, &challengeResponse},
		{"enrollment-request", enrollRaw, &enrollRequest},
		{"enrollment-response", enrolledRaw, &enrollResponse},
	} {
		t.Run(example.name, func(t *testing.T) {
			before := reflect.ValueOf(example.into).Elem().Interface()
			if err := example.into.UnmarshalJSON(example.raw); err != nil || !reflect.DeepEqual(before, reflect.ValueOf(example.into).Elem().Interface()) {
				t.Fatal("valid device wire failed", err)
			}
			var members map[string]json.RawMessage
			if err := json.Unmarshal(example.raw, &members); err != nil {
				t.Fatal(err)
			}
			invalid := [][]byte{[]byte(`null`), []byte(`[]`), append(append([]byte{}, example.raw...), '{', '}'), []byte(`{"x":"\ud800"}`), append(append([]byte{}, example.raw...), 0xff)}
			invalid = append(invalid, []byte(strings.TrimSuffix(string(example.raw), "}")+`,"unknown":"CANARY"}`))
			for key, value := range members {
				for _, badValue := range []string{"null", "1", "{}", "[]", `""`} {
					bad := strings.Replace(string(example.raw), `"`+key+`":`+string(value), `"`+key+`":`+badValue, 1)
					invalid = append(invalid, []byte(bad))
				}
				duplicate := strings.TrimSuffix(string(example.raw), "}") + fmt.Sprintf(`,"\u%04x%s":%s}`, key[0], key[1:], value)
				invalid = append(invalid, []byte(duplicate))
				removed := make(map[string]json.RawMessage, len(members))
				for k, v := range members {
					if k != key {
						removed[k] = v
					}
				}
				missing, _ := json.Marshal(removed)
				invalid = append(invalid, missing)
			}
			for n, raw := range invalid {
				if err := example.into.UnmarshalJSON(raw); err == nil {
					t.Fatalf("invalid device input %d accepted", n)
				}
				if !reflect.DeepEqual(before, reflect.ValueOf(example.into).Elem().Interface()) {
					t.Fatal("failed device decode changed original value")
				}
			}
			full := append(append([]byte{}, example.raw...), bytes.Repeat([]byte(" "), p.MaxDeviceBodyBytes-len(example.raw))...)
			if err := example.into.UnmarshalJSON(full); err != nil {
				t.Fatal("exact raw body limit rejected", err)
			}
			if example.into.UnmarshalJSON(append(full, ' ')) == nil {
				t.Fatal("raw body limit exceeded")
			}
		})
	}
	if enrollRequest.PublicKey() != public || enrollRequest.RunnerID() != first || enrollRequest.RootPath() != "/workspace/private" || enrollRequest.OS() != "linux" || enrollRequest.Arch() != "amd64" {
		t.Fatal("enrollment request identity changed")
	}
	if enrollResponse.RunnerID() != first || enrollResponse.Version() != "2" || enrollResponse.CredentialGeneration() != "1" || enrollResponse.EnrolledAt() != at {
		t.Fatal("enrollment receipt fields changed")
	}
	digest := sha256.Sum256(public[:])
	if enrollResponse.PublicKeyFingerprint() != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatal("public-key fingerprint does not hash original 32 bytes")
	}
}

func TestDeviceHTTPSecretsAndFieldLimits(t *testing.T) {
	secret := strings.Repeat("Q", 42) + "A"
	token, err := p.ParseEnrollmentToken(secret)
	if err != nil {
		t.Fatal(err)
	}
	nonce, _ := p.ParseNonce(secret)
	var public [32]byte
	request, _ := p.NewEnrollmentRequest(first, token, public, "/workspace/ROOT_CANARY", "darwin", "arm64")
	response, _ := p.NewChallengeResponse(nonce, at)
	for _, value := range []any{token, request, response} {
		var logged bytes.Buffer
		slog.New(slog.NewJSONHandler(&logged, nil)).Info("device", "value", value)
		raw, _ := json.Marshal(value)
		text := fmt.Sprintf("%v %+v %#v", value, value, value) + string(raw) + logged.String()
		if strings.Contains(text, secret) || strings.Contains(text, "ROOT_CANARY") {
			t.Fatal("device default output leaked a private field")
		}
	}
	for _, root := range []string{"", "relative", "/a/../b", "/a//b", "/a/", "/bad\\path", "/bad\x00path", "/" + strings.Repeat("a", 4096)} {
		if _, err := p.NewEnrollmentRequest(first, token, public, root, "linux", "amd64"); err == nil {
			t.Fatal("invalid enrollment root accepted")
		}
	}
	for _, version := range []p.Decimal{"0", "1", "02", "+2", "9223372036854775808"} {
		if _, err := p.NewEnrollmentResponse(first, version, "1", at, p.PublicKeyFingerprint(public)); err == nil {
			t.Fatal("uncommitted/overflow enrollment version accepted")
		}
	}
	for _, generation := range []p.Decimal{"0", "01", "-1", "9223372036854775808"} {
		if _, err := p.NewEnrollmentResponse(first, "2", generation, at, p.PublicKeyFingerprint(public)); err == nil {
			t.Fatal("invalid enrollment generation accepted")
		}
	}
	wire, _ := p.EncodeEnrollmentRequest(request)
	for _, broken := range []string{
		strings.Replace(string(wire), `"darwin"`, `"windows"`, 1),
		strings.Replace(string(wire), `"arm64"`, `"riscv64"`, 1),
		strings.Replace(string(wire), base64.RawURLEncoding.EncodeToString(public[:]), base64.StdEncoding.EncodeToString(public[:]), 1),
		strings.Replace(string(wire), secret, secret+"=", 1),
	} {
		if _, err := p.DecodeEnrollmentRequest([]byte(broken)); err == nil {
			t.Fatal("invalid device variant accepted")
		}
	}
}
