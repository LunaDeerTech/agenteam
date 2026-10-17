package document

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strconv"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

const DOCXProfile = "docx_xml:v1"
const wordNamespace = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// ParseDOCX reads WordprocessingML structure without extracting files or
// resolving relationships. Locators address the deterministic extracted text;
// DOCX pagination is deliberately absent because it is layout-dependent.
func ParseDOCX(ctx context.Context, source parser.SourceIdentity, title string, raw []byte) (chunker.Document, error) {
	if ctx == nil || source.Validate() != nil {
		return chunker.Document{}, invalid()
	}
	return parseDOCX(ctx, source, title, raw)
}

// ExtractDOCXText reuses the bounded parser without inventing a Knowledge
// document identity. It emits no locators, performs no I/O and grants no access.
func ExtractDOCXText(ctx context.Context, raw []byte) (string, error) {
	if ctx == nil {
		return "", invalid()
	}
	parsed, err := parseDOCX(ctx, parser.SourceIdentity{}, "", raw)
	if err != nil {
		return "", err
	}
	return parsed.Text, nil
}

func parseDOCX(ctx context.Context, source parser.SourceIdentity, title string, raw []byte) (chunker.Document, error) {
	if err := ctx.Err(); err != nil {
		return chunker.Document{}, err
	}
	if len(raw) > parser.MaxSourceBytes {
		return chunker.Document{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return chunker.Document{}, invalid()
	}
	if len(archive.File) > 256 {
		return chunker.Document{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	var documentXML, stylesXML []byte
	var declared uint64
	seen := map[string]bool{}
	for _, entry := range archive.File {
		if err := ctx.Err(); err != nil {
			return chunker.Document{}, err
		}
		if seen[entry.Name] {
			return chunker.Document{}, invalid()
		}
		seen[entry.Name] = true
		if entry.UncompressedSize64 > 16<<20 || declared > (16<<20)-entry.UncompressedSize64 {
			return chunker.Document{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		declared += entry.UncompressedSize64
		if entry.Name != "word/document.xml" && entry.Name != "word/styles.xml" {
			continue
		}
		if entry.UncompressedSize64 > 4<<20 {
			return chunker.Document{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		reader, err := entry.Open()
		if err != nil {
			return chunker.Document{}, invalid()
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			return chunker.Document{}, invalid()
		}
		if len(body) > 4<<20 {
			return chunker.Document{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		if entry.Name == "word/document.xml" {
			documentXML = body
		} else {
			stylesXML = body
		}
	}
	if len(documentXML) == 0 {
		return chunker.Document{}, invalid()
	}
	styles, err := docxStyles(ctx, stylesXML)
	if err != nil {
		return chunker.Document{}, err
	}
	return docxBody(ctx, source, title, documentXML, styles)
}

type docxStyle struct {
	level  int
	parent string
}

func wordValue(start xml.StartElement, name string) string {
	for _, attribute := range start.Attr {
		if attribute.Name.Local == name && (attribute.Name.Space == wordNamespace || attribute.Name.Space == "") {
			return attribute.Value
		}
	}
	return ""
}
func docxStyles(ctx context.Context, raw []byte) (map[string]int, error) {
	definitions := map[string]docxStyle{}
	out := map[string]int{}
	if len(raw) == 0 {
		return out, nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var current string
	var value docxStyle
	depth := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, invalid()
		}
		switch token := token.(type) {
		case xml.StartElement:
			depth++
			if depth > 128 {
				return nil, invalid()
			}
			if token.Name.Space != wordNamespace {
				continue
			}
			switch token.Name.Local {
			case "style":
				current = wordValue(token, "styleId")
				value = docxStyle{}
				if len(definitions) > 1024 {
					return nil, f.NewFault(f.PayloadTooLarge, f.NotStarted)
				}
			case "basedOn":
				value.parent = wordValue(token, "val")
			case "outlineLvl":
				n, err := strconv.Atoi(wordValue(token, "val"))
				if err == nil && n >= 0 && n < 9 {
					value.level = n + 1
				}
			}
		case xml.EndElement:
			if token.Name.Space == wordNamespace && token.Name.Local == "style" && current != "" {
				definitions[current] = value
				current = ""
			}
			depth--
		}
	}
	for name := range definitions {
		seen := map[string]bool{}
		at := name
		for range 32 {
			if at == "" || seen[at] {
				break
			}
			seen[at] = true
			v := definitions[at]
			if v.level != 0 {
				out[name] = v.level
				break
			}
			at = v.parent
		}
	}
	return out, nil
}
func headingLevel(style string, styles map[string]int) int {
	if n := styles[style]; n != 0 {
		return n
	}
	name := strings.ToLower(strings.ReplaceAll(style, " ", ""))
	if strings.HasPrefix(name, "heading") {
		n, err := strconv.Atoi(strings.TrimPrefix(name, "heading"))
		if err == nil && n > 0 && n < 10 {
			return n
		}
	}
	return 0
}
func docxBody(ctx context.Context, source parser.SourceIdentity, title string, raw []byte, styles map[string]int) (chunker.Document, error) {
	out := chunker.Document{Source: source, Title: title, ParserProfile: DOCXProfile, Elements: []chunker.Element{}}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var text, paragraph, table, row, cell strings.Builder
	var path []string
	var levels []int
	depth, paragraphDepth, tableDepth, rowDepth, cellDepth, textDepth, deletionDepth := 0, 0, 0, 0, 0, 0, 0
	style := ""
	list := false
	bodySeen := false
	appendElement := func(kind, value string, level int) error {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		if len(out.Elements) >= parser.MaxParagraphs || text.Len()+len(value)+2 > parser.MaxSourceBytes {
			return f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		if level > 0 {
			for len(levels) > 0 && levels[len(levels)-1] >= level {
				levels = levels[:len(levels)-1]
				path = path[:len(path)-1]
			}
			levels = append(levels, level)
			path = append(path, strings.TrimSpace(value))
			kind = "heading"
		}
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		start := text.Len()
		text.WriteString(value)
		out.Elements = append(out.Elements, chunker.Element{Kind: kind, Ordinal: len(out.Elements) + 1, Bytes: parser.ByteRange{Start: start, End: text.Len()}, HeadingPath: append([]string(nil), path...)})
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return chunker.Document{}, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return chunker.Document{}, invalid()
		}
		switch token := token.(type) {
		case xml.StartElement:
			depth++
			if depth > 128 {
				return chunker.Document{}, invalid()
			}
			if token.Name.Space != wordNamespace {
				continue
			}
			switch token.Name.Local {
			case "body":
				bodySeen = true
			case "del":
				if deletionDepth == 0 {
					deletionDepth = depth
				}
			case "tbl":
				if tableDepth == 0 {
					tableDepth = depth
					table.Reset()
				}
			case "tr":
				if tableDepth > 0 && rowDepth == 0 {
					rowDepth = depth
					row.Reset()
				}
			case "tc":
				if rowDepth > 0 && cellDepth == 0 {
					cellDepth = depth
					cell.Reset()
				}
			case "p":
				if paragraphDepth != 0 {
					return chunker.Document{}, invalid()
				}
				paragraphDepth = depth
				paragraph.Reset()
				style = ""
				list = false
			case "pStyle":
				style = wordValue(token, "val")
			case "numPr":
				list = true
			case "t":
				if paragraphDepth > 0 && deletionDepth == 0 {
					textDepth = depth
				}
			case "tab":
				if paragraphDepth > 0 && deletionDepth == 0 {
					paragraph.WriteByte('\t')
				}
			case "br", "cr":
				if paragraphDepth > 0 && deletionDepth == 0 {
					paragraph.WriteByte('\n')
				}
			}
		case xml.CharData:
			if textDepth > 0 && deletionDepth == 0 {
				if paragraph.Len()+len(token) > parser.MaxSourceBytes {
					return chunker.Document{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
				}
				paragraph.Write(token)
			}
		case xml.EndElement:
			if token.Name.Space == wordNamespace {
				switch token.Name.Local {
				case "t":
					if textDepth == depth {
						textDepth = 0
					}
				case "del":
					if deletionDepth == depth {
						deletionDepth = 0
					}
				case "p":
					if paragraphDepth == depth {
						value := paragraph.String()
						paragraphDepth = 0
						if tableDepth > 0 {
							if cell.Len() > 0 {
								cell.WriteByte('\n')
							}
							cell.WriteString(value)
						} else {
							kind := "paragraph"
							if list {
								kind = "list"
							}
							if err := appendElement(kind, value, headingLevel(style, styles)); err != nil {
								return chunker.Document{}, err
							}
						}
					}
				case "tc":
					if cellDepth == depth {
						if row.Len() > 0 {
							row.WriteByte('\t')
						}
						row.WriteString(cell.String())
						cellDepth = 0
					}
				case "tr":
					if rowDepth == depth {
						if table.Len() > 0 {
							table.WriteByte('\n')
						}
						table.WriteString(row.String())
						rowDepth = 0
					}
				case "tbl":
					if tableDepth == depth {
						if err := appendElement("table", table.String(), 0); err != nil {
							return chunker.Document{}, err
						}
						tableDepth = 0
					}
				}
			}
			depth--
		}
	}
	if !bodySeen {
		return chunker.Document{}, invalid()
	}
	out.Text = text.String()
	return out, nil
}
