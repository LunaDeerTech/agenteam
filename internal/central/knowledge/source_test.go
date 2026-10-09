package knowledge

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestReadTextByteOffsetsAndCharacterBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, source    string
		offset          f.Progress
		limit           int
		text            string
		next            f.Progress
		truncated, fail bool
	}{
		{"empty", "", 0, 1, "", 0, false, false},
		{"complete", "文a", 0, 4, "文a", 4, false, false},
		{"do not split", "a文b", 0, 3, "a", 1, true, false},
		{"too small", "文", 0, 1, "", 0, true, false},
		{"offset", "文ab", 3, 1, "a", 4, true, false},
		{"eof offset", "文", 3, 4, "", 3, false, false},
		{"past eof", "文", 4, 4, "", 0, false, true},
		{"inside character", "文ab", 1, 4, "", 0, false, true},
		{"invalid utf8", "\xff", 0, 4, "", 0, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, err := readText(strings.NewReader(tt.source), kc.ReadRequest{ByteOffset: tt.offset, MaxBytes: tt.limit})
			if tt.fail {
				if err == nil {
					t.Fatal("invalid read accepted")
				}
				return
			}
			if err != nil || out.Text != tt.text || out.NextByteOffset != tt.next || out.Truncated != tt.truncated {
				t.Fatalf("got %#v, err=%v", out, err)
			}
		})
	}
}

type closeProbe struct {
	err   error
	calls int
}

func (*closeProbe) Read([]byte) (int, error) { return 0, io.EOF }
func (r *closeProbe) Close() error           { r.calls++; return r.err }
func TestCanonicalUnderlyingCloseFailureDoesNotReportCallJoined(t *testing.T) {
	scope, err := id.InProject(newID[id.Project](t))
	if err != nil {
		t.Fatal(err)
	}
	now, err := f.NewInstant(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("controlled close failure")
	probe := &closeProbe{err: closeErr}
	reader, err := oc.NewObjectReader(oc.ObjectMeta{ID: newID[oc.StoredObject](t), Scope: scope, MediaType: kc.PlainText, ByteSize: 0, SHA256: f.Digest("sha256:" + strings.Repeat("0", 64)), State: oc.Available, Version: 1, CreatedAt: now}, nil, probe)
	if err != nil {
		t.Fatal(err)
	}
	joined := 0
	tracked := &trackedRead{body: reader, done: func() { joined++ }}
	if err = tracked.Close(); !errors.Is(err, closeErr) || joined != 0 {
		t.Fatal("failure became join", err, joined)
	}
	probe.err = nil
	if err = tracked.Close(); err != nil || joined != 1 {
		t.Fatal("real close did not join", err, joined)
	}
	if err = tracked.Close(); err != nil || joined != 1 {
		t.Fatal("double join", err, joined)
	}
}
