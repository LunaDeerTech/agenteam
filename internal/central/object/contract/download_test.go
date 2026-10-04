package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestDownloadURLProjectionIsHumanOnlyAndOpaque(t *testing.T) {
	user, _ := foundation.NewID[identity.User]()
	session, _ := foundation.NewID[identity.Session]()
	human, _ := identity.NewHuman(user, session)
	otherID, _ := foundation.NewID[identity.User]()
	other, _ := identity.NewHuman(otherID, session)
	expires, _ := foundation.NewInstant(time.Now().Add(time.Minute))
	const canary = "a.private-url-canary.signature"
	url, e := NewPrivateSignedURL(user, canary, expires)
	if e != nil {
		t.Fatal(e)
	}
	response, e := url.ForHuman(human)
	if e != nil || !strings.HasSuffix(response.URL, canary) {
		t.Fatal("authorized explicit response")
	}
	if _, e = url.ForHuman(other); e == nil {
		t.Fatal("cross-user URL projection")
	}
	var out bytes.Buffer
	for _, v := range []any{url, &url, struct{ url PrivateSignedURL }{url}, struct{ value any }{url}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			fmt.Fprintf(&out, format, v)
		}
		b, _ := json.Marshal(v)
		out.Write(b)
		slog.New(slog.NewTextHandler(&out, nil)).Info("value", "v", v)
		slog.New(slog.NewJSONHandler(&out, nil)).Info("value", "v", v)
	}
	if strings.Contains(out.String(), canary) {
		t.Fatal("URL leaked")
	}
	if json.Unmarshal([]byte(`"private_download_url"`), &url) == nil {
		t.Fatal("JSON created URL")
	}
}
func TestDownloadEventClosedPhasesAndCounts(t *testing.T) {
	grant, _ := foundation.NewID[DownloadGrant]()
	attempt, _ := foundation.NewID[DownloadAttempt]()
	valid := []DownloadEvent{{GrantID: grant, Phase: DownloadIssued, Length: 10}, {GrantID: grant, AttemptID: attempt, Phase: DownloadStarted, Length: 10}, {GrantID: grant, AttemptID: attempt, Phase: DownloadSent, Length: 3, SentBytes: 3}, {GrantID: grant, AttemptID: attempt, Phase: DownloadFailed, Length: 10, SentBytes: 4, Failure: DownloadReadFailed}}
	for _, event := range valid {
		if event.Validate() != nil {
			t.Fatal("valid stage rejected")
		}
	}
	bad := []DownloadEvent{valid[0], valid[1], valid[2], valid[3]}
	bad[0].AttemptID = attempt
	bad[1].SentBytes = 1
	bad[2].SentBytes = 2
	bad[3].Failure = "raw secret"
	for _, event := range bad {
		if event.Validate() == nil {
			t.Fatal("inconsistent stage accepted")
		}
	}
	for _, name := range []string{"", strings.Repeat("é", 128), "a/b", "a\\b", "x\r\nInjected", "x\x00", string([]byte{0xff})} {
		if ValidateFilename(name) == nil {
			t.Fatal("unsafe filename accepted")
		}
	}
	if ValidateFilename(strings.Repeat("é", 127)+"x") != nil {
		t.Fatal("255 byte filename rejected")
	}
}
