package skillhttp

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func TestSkillOwnerHTTPProjectionRejectsMalformedResults(t *testing.T) {
	for _, mode := range []string{"nil-directory", "empty-directory", "two-items", "foreign-project", "wrong-target", "bad-name", "bad-normalized", "bad-description", "zero-revision", "zero-version"} {
		t.Run(mode, func(t *testing.T) {
			m := testMetadata()
			items := []sc.Metadata{m}
			target := m.ID
			switch mode {
			case "nil-directory":
				items = nil
			case "empty-directory":
				items = []sc.Metadata{}
			case "two-items":
				items = append(items, m)
			case "foreign-project":
				m.ProjectID = testID[id.Project](99)
			case "wrong-target":
				target = testID[pc.Skill](98)
			case "bad-name":
				m.Name = "bad\n"
			case "bad-normalized":
				m.NormalizedName = "not-the-derived-name"
			case "bad-description":
				m.Description = "private\x00canary"
			case "zero-revision":
				m.CurrentRevision = 0
			case "zero-version":
				m.Version = 0
			}
			var raw []byte
			var err error
			if strings.HasSuffix(mode, "directory") || mode == "two-items" {
				raw, err = encodeDirectory(context.Background(), testMetadata().ProjectID, items)
			} else {
				raw, err = encodeDetail(context.Background(), testMetadata().ProjectID, target, m)
			}
			var fault *f.Fault
			if len(raw) != 0 || !errors.As(err, &fault) || fault.Code != f.DependencyUnavailable {
				t.Fatal("bad projection published a candidate")
			}
		})
	}
}
func TestSkillOwnerHTTPRepresentationBoundAndCancellation(t *testing.T) {
	m := testMetadata()
	m.Name = strings.Repeat("<", sc.MaxNameBytes)
	m.NormalizedName = m.Name
	m.Description = strings.Repeat("<", sc.MaxDescriptionBytes)
	if m.Validate() != nil {
		t.Fatal("legal maximum fixture")
	}
	raw, err := encodeDirectory(context.Background(), m.ProjectID, []sc.Metadata{m})
	if err != nil || len(raw) > maxRepresentationBytes || len(raw) < sc.MaxDescriptionBytes*6 {
		t.Fatal("valid JSON escaping bound", len(raw), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if raw, err = encodeDetail(ctx, m.ProjectID, m.ID, m); len(raw) != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled encoding published")
	}
}
