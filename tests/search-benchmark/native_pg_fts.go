// Package searchbenchmark contains only the fixed offline lexical adapter.
// It never receives relevance judgments or production database configuration.
package searchbenchmark

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

const nativeRevision = "native-pg-fts-v1"
const datasetRevision = "lexical-v2"
const maxInputBytes = 2 << 20
const maxLineBytes = 64 << 10

var exportID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type inputText struct{ ID, Text string }
type nativeInput struct {
	Sources, Queries []inputText
	SHA256           map[string]string
}

// Read only the completed, answer-free export, with the evaluator's original
// byte/row limits. Do not import its dataset loader or discover sibling files.
func loadNativeInput(directory string) (nativeInput, error) {
	var result nativeInput
	if !filepath.IsAbs(directory) {
		return result, errors.New("absolute export directory required")
	}
	result.SHA256 = make(map[string]string)
	read := func(name string) ([]byte, error) {
		raw, err := readNativeFile(filepath.Join(directory, name))
		if err == nil {
			digest := sha256.Sum256(raw)
			result.SHA256[name] = hex.EncodeToString(digest[:])
		}
		return raw, err
	}
	marker, err := read("complete.json")
	if err != nil {
		return result, err
	}
	fields, err := nativeFields(marker, "format_version", "kind", "files")
	var kind string
	var files []string
	if err != nil || string(fields["format_version"]) != "1" || json.Unmarshal(fields["kind"], &kind) != nil || kind != "export" || json.Unmarshal(fields["files"], &files) != nil || !slices.Equal(files, []string{"corpus.jsonl", "queries.jsonl"}) {
		return result, errors.New("completed answer-free export required")
	}
	for _, source := range []struct {
		name, id, text string
		count, limit   int
		target         *[]inputText
	}{
		{"corpus.jsonl", "source_id", "index_text", 64, 2*160 + 4096 + 3, &result.Sources},
		{"queries.jsonl", "query_id", "text", 72, 256, &result.Queries},
	} {
		raw, err := read(source.name)
		if err != nil {
			return result, err
		}
		lines := bytes.Split(raw[:len(raw)-1], []byte{'\n'})
		if len(lines) != source.count {
			return result, errors.New("fixed export row count required")
		}
		previous := ""
		for _, line := range lines {
			fields, err := nativeFields(line, "dataset_revision", source.id, source.text)
			values := make(map[string]string, 3)
			if err != nil {
				return result, err
			}
			for key, value := range fields {
				var decoded string
				if len(value) == 0 || value[0] != '"' || json.Unmarshal(value, &decoded) != nil || strings.ContainsAny(decoded, "\x00\r") || strings.TrimSpace(decoded) == "" {
					return result, errors.New("invalid export text")
				}
				values[key] = decoded
			}
			id := values[source.id]
			if values["dataset_revision"] != datasetRevision || !exportID.MatchString(id) || id <= previous || len(values[source.text]) > source.limit {
				return result, errors.New("invalid export revision, order or text limit")
			}
			*source.target = append(*source.target, inputText{id, values[source.text]})
			previous = id
		}
	}
	return result, nil
}

func readNativeFile(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("export file unreadable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("regular export file required")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxInputBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxInputBytes || !utf8.Valid(raw) || raw[len(raw)-1] != '\n' || bytes.ContainsAny(raw, "\r\x00") {
		return nil, errors.New("invalid export encoding, framing or size")
	}
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		if len(line) > maxLineBytes {
			return nil, errors.New("export row too large")
		}
	}
	return raw, nil
}

// All fields are top-level; values are decoded separately into exact types.
// Go's JSON decoder otherwise silently replaces unpaired UTF-16 escapes.
func nativeFields(raw []byte, names ...string) (map[string]json.RawMessage, error) {
	fail := func() (map[string]json.RawMessage, error) { return nil, errors.New("invalid closed export JSON") }
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) || raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return fail()
		}
		n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil || n >= 0xdc00 && n <= 0xdfff {
			return fail()
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+10 >= len(raw) || string(raw[i+5:i+7]) != `\u` {
				return fail()
			}
			low, err := strconv.ParseUint(string(raw[i+7:i+11]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return fail()
			}
			i += 6
		}
		i += 4
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return fail()
	}
	fields := make(map[string]json.RawMessage, len(names))
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || !slices.Contains(names, name) || fields[name] != nil {
			return fail()
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return fail()
		}
		fields[name] = value
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || len(fields) != len(names) {
		return fail()
	}
	if _, err := d.Token(); err != io.EOF {
		return fail()
	}
	return fields, nil
}

// Publish only after actual SQL and independent persisted-input checks. A
// completion marker is last; a failed write owns only this new directory.
func publishNative(directory string, outputs map[string]any) (err error) {
	if !filepath.IsAbs(directory) || os.Mkdir(directory, 0700) != nil {
		return errors.New("absolute new candidate directory required")
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(directory)
		}
	}()
	names := make([]string, 0, len(outputs))
	for name, value := range outputs {
		if filepath.Base(name) != name || name == "complete.json" {
			return errors.New("invalid candidate output name")
		}
		raw, encodeErr := json.MarshalIndent(value, "", "  ")
		if encodeErr != nil || len(raw)+1 > maxInputBytes {
			return errors.New("candidate output invalid or too large")
		}
		if err = os.WriteFile(filepath.Join(directory, name), append(raw, '\n'), 0600); err != nil {
			return errors.New("candidate output write failed")
		}
		names = append(names, name)
	}
	slices.Sort(names)
	raw, _ := json.Marshal(map[string]any{"format_version": 1, "kind": "native_pg_fts", "files": names})
	if err = os.WriteFile(filepath.Join(directory, "complete.json"), append(raw, '\n'), 0600); err != nil {
		return errors.New("candidate completion write failed")
	}
	return nil
}
