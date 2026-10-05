package skill

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

const AddSkillsBundleID = "builtin.add-skills.v1"
const AddSkillsName = "Add Skills"
const AddSkillsNormalizedName = "add-skills"
const MaxBuiltinBytes = 256 << 10
const AddSkillsPackageSHA256 f.Digest = "sha256:a67cef2cf755baa48880ea1727444ab060ac6237e5505e99081e868c91f1dcf1"
const AddSkillsEntrySHA256 f.Digest = "sha256:a5f2416d9531ca0d450d97fd9d7064187c3d865573b9b20d874d146f1ee9663d"

//go:embed builtin/add-skills/v1/SKILL.md
var addSkillsText string

type bundleData struct {
	id       string
	revision f.Revision
	content  Package
}
type BuiltinBundle struct{ data func() bundleData }

// AddSkills prepares the real pinned bytes. A returned bundle is not a Skill
// revision or Project initialization receipt. Publication requires the later
// real D05/D08 services, current authority and an atomic business transaction.
func AddSkills(ctx context.Context) (BuiltinBundle, error) {
	if sum([]byte(addSkillsText)) != AddSkillsEntrySHA256 {
		return BuiltinBundle{}, invalidPackage()
	}
	input, err := sc.NewTextFiles([]sc.TextFile{{Path: sc.EntryPath, UTF8Text: addSkillsText}})
	if err != nil {
		return BuiltinBundle{}, err
	}
	content, err := BuildPackage(ctx, input)
	if err != nil {
		return BuiltinBundle{}, err
	}
	size, _ := content.Size()
	digest, _ := content.Digest()
	entry, _ := content.Entry()
	name, err := sc.NormalizeName(entry.Name)
	if size > MaxBuiltinBytes || digest != AddSkillsPackageSHA256 || entry.Name != AddSkillsName || err != nil || name != AddSkillsNormalizedName {
		return BuiltinBundle{}, invalidPackage()
	}
	data := bundleData{AddSkillsBundleID, 1, content}
	return BuiltinBundle{func() bundleData { return data }}, nil
}
func (b BuiltinBundle) Validate() error {
	if b.data == nil {
		return invalidPackage()
	}
	return nil
}
func (b BuiltinBundle) ID() (string, error) {
	if b.Validate() != nil {
		return "", invalidPackage()
	}
	return b.data().id, nil
}
func (b BuiltinBundle) Revision() (f.Revision, error) {
	if b.Validate() != nil {
		return 0, invalidPackage()
	}
	return b.data().revision, nil
}
func (b BuiltinBundle) Package() (Package, error) {
	if b.Validate() != nil {
		return Package{}, invalidPackage()
	}
	return b.data().content, nil
}
func (BuiltinBundle) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_builtin_bundle") }
func (BuiltinBundle) MarshalJSON() ([]byte, error) { return []byte(`"skill_builtin_bundle"`), nil }
func (*BuiltinBundle) UnmarshalJSON([]byte) error  { return invalidPackage() }
func (BuiltinBundle) LogValue() slog.Value         { return slog.StringValue("skill_builtin_bundle") }
