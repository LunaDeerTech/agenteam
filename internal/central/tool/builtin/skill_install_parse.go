// Package builtin adapts domain capabilities. Definitions and parsed arguments
// do not establish a registered backend or runtime execution permission.
package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

const MaxSkillInstallArguments = 1 << 20

type skillInstallPackageData struct {
	packageValue skill.Package
	facts        SkillInstallPackageFacts
}

// SkillInstallPackage retains the domain's immutable, validated package. It is
// neither a command receipt nor evidence of an authorized Tool Operation.
type SkillInstallPackage struct {
	data func() skillInstallPackageData
}

// SkillInstallPackageFacts is an explicit metadata projection, not a grant.
type SkillInstallPackageFacts struct {
	NormalizedName string
	PackageSHA256  f.Digest
	ManifestSHA256 f.Digest
}

func skillInstallInvalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }

func skillInstallContext(ctx context.Context) error {
	if ctx == nil {
		return skillInstallInvalid()
	}
	return ctx.Err()
}

func (p SkillInstallPackage) Validate() error {
	if p.data == nil {
		return skillInstallInvalid()
	}
	return p.data().packageValue.Validate()
}

func (p SkillInstallPackage) Package() (skill.Package, error) {
	if err := p.Validate(); err != nil {
		return skill.Package{}, err
	}
	return p.data().packageValue, nil
}

func (p SkillInstallPackage) Facts() (SkillInstallPackageFacts, error) {
	if err := p.Validate(); err != nil {
		return SkillInstallPackageFacts{}, err
	}
	return p.data().facts, nil
}

func (SkillInstallPackage) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_install_package")
}
func (SkillInstallPackage) LogValue() slog.Value { return slog.StringValue("skill_install_package") }
func (SkillInstallPackage) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_install_package"`), nil
}
func (*SkillInstallPackage) UnmarshalJSON([]byte) error { return skillInstallInvalid() }

// ParseSkillInstall implements only the closed text_files/create v1 schema.
// The original arguments are bounded before token allocation. Domain package
// validation/encoding remains exclusively in the Skill implementation.
func ParseSkillInstall(ctx context.Context, raw []byte) (SkillInstallPackage, error) {
	if err := skillInstallContext(ctx); err != nil {
		return SkillInstallPackage{}, err
	}
	if len(raw) > MaxSkillInstallArguments {
		return SkillInstallPackage{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	if len(raw) == 0 || !utf8.Valid(raw) {
		return SkillInstallPackage{}, skillInstallInvalid()
	}
	if err := skillInstallUnicode(ctx, raw); err != nil {
		return SkillInstallPackage{}, err
	}
	p := skillInstallParser{ctx: ctx, decoder: json.NewDecoder(bytes.NewReader(raw))}
	var files []sc.TextFile
	err := p.object(func(key string) error {
		switch key {
		case "mode":
			v, err := p.text()
			if err != nil || v != "create" {
				return skillInstallInvalid()
			}
		case "source":
			return p.object(func(key string) error {
				switch key {
				case "kind":
					v, err := p.text()
					if err != nil || v != "text_files" {
						return skillInstallInvalid()
					}
				case "files":
					var err error
					files, err = p.files()
					return err
				default:
					return skillInstallInvalid()
				}
				return nil
			}, "kind", "files")
		default:
			return skillInstallInvalid()
		}
		return nil
	}, "source", "mode")
	if err != nil {
		if cancel := ctx.Err(); cancel != nil {
			return SkillInstallPackage{}, cancel
		}
		return SkillInstallPackage{}, skillInstallInvalid()
	}
	if _, err = p.decoder.Token(); err != io.EOF {
		return SkillInstallPackage{}, skillInstallInvalid()
	}
	if err = ctx.Err(); err != nil {
		return SkillInstallPackage{}, err
	}
	text, err := sc.NewTextFiles(files)
	if err != nil {
		return SkillInstallPackage{}, err
	}
	pkg, err := skill.BuildPackage(ctx, text)
	if err != nil {
		return SkillInstallPackage{}, err
	}
	entry, err := pkg.Entry()
	if err != nil {
		return SkillInstallPackage{}, err
	}
	normalized, err := sc.NormalizeName(entry.Name)
	if err != nil {
		return SkillInstallPackage{}, err
	}
	manifest, err := pkg.Manifest()
	if err != nil {
		return SkillInstallPackage{}, err
	}
	manifestDigest, err := manifest.Digest()
	if err != nil {
		return SkillInstallPackage{}, err
	}
	packageDigest, err := pkg.Digest()
	if err != nil {
		return SkillInstallPackage{}, err
	}
	if err = ctx.Err(); err != nil {
		return SkillInstallPackage{}, err
	}
	data := skillInstallPackageData{pkg, SkillInstallPackageFacts{normalized, packageDigest, manifestDigest}}
	return SkillInstallPackage{data: func() skillInstallPackageData { return data }}, nil
}

type skillInstallParser struct {
	ctx     context.Context
	decoder *json.Decoder
}

func (p *skillInstallParser) token() (json.Token, error) {
	if err := p.ctx.Err(); err != nil {
		return nil, err
	}
	v, err := p.decoder.Token()
	if err != nil {
		return nil, skillInstallInvalid()
	}
	return v, nil
}

func (p *skillInstallParser) delimiter(want json.Delim) error {
	v, err := p.token()
	if err != nil {
		return err
	}
	if v != want {
		return skillInstallInvalid()
	}
	return nil
}

func (p *skillInstallParser) text() (string, error) {
	v, err := p.token()
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", skillInstallInvalid()
	}
	return s, nil
}

func (p *skillInstallParser) object(field func(string) error, required ...string) error {
	if err := p.delimiter('{'); err != nil {
		return err
	}
	seen := make(map[string]bool, len(required))
	for p.decoder.More() {
		key, err := p.text()
		if err != nil || seen[key] {
			return skillInstallInvalid()
		}
		allowed := false
		for _, name := range required {
			allowed = allowed || key == name
		}
		if !allowed {
			return skillInstallInvalid()
		}
		seen[key] = true
		if err = field(key); err != nil {
			return err
		}
	}
	if len(seen) != len(required) {
		return skillInstallInvalid()
	}
	return p.delimiter('}')
}

func (p *skillInstallParser) files() ([]sc.TextFile, error) {
	if err := p.delimiter('['); err != nil {
		return nil, err
	}
	files := make([]sc.TextFile, 0)
	for p.decoder.More() {
		if len(files) == sc.MaxFiles {
			return nil, skillInstallInvalid()
		}
		var file sc.TextFile
		err := p.object(func(key string) error {
			v, err := p.text()
			if err != nil {
				return err
			}
			if key == "path" {
				file.Path = v
			} else {
				file.UTF8Text = v
			}
			return nil
		}, "path", "utf8_text")
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, skillInstallInvalid()
	}
	if err := p.delimiter(']'); err != nil {
		return nil, err
	}
	return files, nil
}

// encoding/json accepts unpaired surrogate escapes as U+FFFD. Reject those
// spellings before decoding while preserving valid pairs and escaped slashes.
// Remaining JSON syntax is checked by the closed token parser above.
func skillInstallUnicode(ctx context.Context, raw []byte) error {
	inside := false
	for i := 0; i < len(raw); i++ {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if raw[i] == '"' {
			inside = !inside
			continue
		}
		if !inside || raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return skillInstallInvalid()
		}
		if raw[i] != 'u' {
			continue
		}
		u, ok := skillInstallHex4(raw, i+1)
		if !ok {
			return skillInstallInvalid()
		}
		i += 4
		if u >= 0xDC00 && u <= 0xDFFF {
			return skillInstallInvalid()
		}
		if u < 0xD800 || u > 0xDBFF {
			continue
		}
		if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
			return skillInstallInvalid()
		}
		low, ok := skillInstallHex4(raw, i+3)
		if !ok || low < 0xDC00 || low > 0xDFFF {
			return skillInstallInvalid()
		}
		i += 6
	}
	return ctx.Err()
}

func skillInstallHex4(raw []byte, start int) (uint16, bool) {
	if start+4 > len(raw) {
		return 0, false
	}
	var v uint16
	for _, c := range raw[start : start+4] {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= uint16(c - '0')
		case c >= 'a' && c <= 'f':
			v |= uint16(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v |= uint16(c-'A') + 10
		default:
			return 0, false
		}
	}
	return v, true
}
