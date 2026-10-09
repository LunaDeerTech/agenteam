//go:build integration

package account_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"syscall"
	"unicode/utf8"
)

const projectModelsWebProtocol = "project-owner-models.v1"

// Open first, then validate and read that same descriptor. A concurrent atomic
// publication may replace the pathname; it cannot change the descriptor's
// identity. NONBLOCK prevents an unexpected FIFO from stalling before fstat.
func projectModelsWebReadPrivate(path string, limit int64) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() < 1 || info.Size() > limit {
		return nil, errors.New("owned private input rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) > limit || !utf8.Valid(raw) {
		clear(raw)
		return nil, errors.New("owned private input rejected")
	}
	if err := file.Close(); err != nil {
		clear(raw)
		return nil, errors.New("owned private input close failed")
	}
	return raw, nil
}

// encoding/json alone accepts duplicate keys and replaces malformed strings.
// Walk the complete value before decoding into typed, closed envelopes.
func projectModelsWebJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return errors.New("owned JSON invalid")
	}
	// JSON permits escapes but not unpaired UTF-16 surrogates in our protocol.
	// Validate escape pairs before encoding/json can replace them with U+FFFD.
	quoted := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) || raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return errors.New("owned JSON escape invalid")
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil || code >= 0xdc00 && code <= 0xdfff {
			return errors.New("owned JSON escape invalid")
		}
		i += 4
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return errors.New("owned JSON escape invalid")
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return errors.New("owned JSON escape invalid")
			}
			i += 6
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return errors.New("owned JSON depth exceeded")
		}
		token, err := d.Token()
		if err != nil {
			return errors.New("owned JSON invalid")
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return errors.New("owned JSON duplicate member")
				}
				seen[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			if end, err := d.Token(); err != nil || end != json.Delim('}') {
				return errors.New("owned JSON invalid")
			}
		case json.Delim('['):
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			if end, err := d.Token(); err != nil || end != json.Delim(']') {
				return errors.New("owned JSON invalid")
			}
		case json.Delim('}'), json.Delim(']'):
			return errors.New("owned JSON invalid")
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("owned JSON trailing value")
	}
	return nil
}

func projectModelsWebObject(raw []byte, members ...string) (map[string]json.RawMessage, error) {
	if err := projectModelsWebJSON(raw); err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil || out == nil || len(out) != len(members) {
		return nil, errors.New("owned JSON object rejected")
	}
	for _, member := range members {
		if _, ok := out[member]; !ok {
			return nil, errors.New("owned JSON object rejected")
		}
	}
	return out, nil
}

type projectModelsWebIPC struct {
	Protocol  string          `json:"protocol"`
	InputHash string          `json:"input_hash"`
	Sequence  int             `json:"sequence"`
	Action    string          `json:"action"`
	Args      json.RawMessage `json:"args"`
}

func decodeProjectModelsWebIPC(raw []byte, inputHash string, next int) (projectModelsWebIPC, string) {
	var out projectModelsWebIPC
	if len(raw) > 8192 {
		return out, "invalid_envelope"
	}
	if _, err := projectModelsWebObject(raw, "protocol", "input_hash", "sequence", "action", "args"); err != nil || json.Unmarshal(raw, &out) != nil || out.Protocol != projectModelsWebProtocol || out.InputHash != inputHash {
		return out, "invalid_envelope"
	}
	if out.Sequence != next || next < 1 || next > 128 {
		return out, "invalid_sequence"
	}
	var members []string
	switch out.Action {
	case "snapshot":
		members = []string{"project"}
	case "arm":
		members = []string{"operation", "project", "target_id", "query", "effect"}
	case "control-state":
		members = []string{"arm_id"}
	case "release":
		members = []string{"arm_id", "request_token"}
	case "counts":
		members = []string{}
	case "logout":
		members = []string{"session_id"}
	case "archive-recovery-project":
		members = []string{"project", "expected_version"}
	case "reference-fact":
		members = []string{"project", "state"}
	case "rename-reuse":
		members = []string{"project"}
	default:
		return out, "invalid_action"
	}
	if _, err := projectModelsWebObject(out.Args, members...); err != nil {
		return out, "invalid_arguments"
	}
	return out, ""
}
