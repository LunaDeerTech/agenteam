package work

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type independentPrivateRow struct{ values []any }

func (r independentPrivateRow) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return fmt.Errorf("independent private column count")
	}
	for n, v := range r.values {
		reflect.ValueOf(dest[n]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

// Change one exact JSON object at a path; RawMessage preserves duplicate keys
// inserted by edit so the real persisted decoder, not this helper, judges them.
func independentPrivateEdit(t *testing.T, raw []byte, path []string, edit func(map[string]json.RawMessage) []byte) []byte {
	t.Helper()
	if len(path) == 0 {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || object == nil {
			t.Fatal("independent object path does not exist")
		}
		return edit(object)
	}
	var value any
	if n, err := strconv.Atoi(path[0]); err == nil {
		var array []json.RawMessage
		if json.Unmarshal(raw, &array) != nil || n < 0 || n >= len(array) {
			t.Fatal("independent array path does not exist")
		}
		array[n] = independentPrivateEdit(t, array[n], path[1:], edit)
		value = array
	} else {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || object == nil {
			t.Fatal("independent parent path does not exist")
		}
		object[path[0]] = independentPrivateEdit(t, object[path[0]], path[1:], edit)
		value = object
	}
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatal("independent parent encoding failed")
	}
	return out
}

func TestIndependentTaskPrivateShapeMatrix(t *testing.T) {
	// The seed helper only assembles valid data. The expectations are defined
	// below from exact persisted schemas, through the actual scan entry point.
	record, actor, _ := pureTaskRecord(t)
	request, err := json.Marshal(record.Input)
	if err != nil {
		t.Fatal("independent seed request encoding")
	}
	plan, err := json.Marshal(record.Plan)
	if err != nil {
		t.Fatal("independent seed plan encoding")
	}
	te, ev := record.TaskEventID.String(), record.EventID.String()
	scan := func(req, body []byte) error {
		row := independentPrivateRow{[]any{record.ID.String(), record.Project.String(), record.User.String(), record.Command, record.Key, record.Semantic, req, record.Revision, record.State, body, &te, &ev, []byte(nil), record.Created.Time(), (*time.Time)(nil)}}
		out, e := scanTaskCommand(row)
		if e != nil {
			return e
		}
		return validateTaskRecord(out, actor)
	}
	if scan(request, plan) != nil {
		t.Fatal("independent valid persisted row rejected")
	}
	layers := []struct {
		name, fieldList, nullable string
		request                   bool
		path                      []string
	}{
		{"input", "command project_id target_id actor_user_id expected_version create update reorder", " expected_version update reorder ", true, nil},
		{"plan", "before after placement groups query_generation task_event header payload", " before ", false, nil},
		{"placement", "milestone_id sprint_id state", "", false, []string{"placement"}},
		{"group-plan", "group before after generation", "", false, []string{"groups", "0"}},
		{"group", "sprint_id state priority", "", false, []string{"groups", "0", "group"}},
		{"rank-item", "ID Rank", "", false, []string{"groups", "0", "after", "0"}},
		{"header", "event_id event_type schema_version occurred_at scope aggregate_type aggregate_id aggregate_version", "", false, []string{"header"}},
		{"scope", "kind project_id", "", false, []string{"header", "scope"}},
	}
	for _, layer := range layers {
		for _, field := range strings.Fields(layer.fieldList) {
			for _, kind := range []string{"case", "alias-pair", "exact-duplicate", "missing", "null", "unknown"} {
				if kind == "null" && strings.Contains(layer.nullable, " "+field+" ") {
					continue
				}
				t.Run(layer.name+"/"+field+"/"+kind, func(t *testing.T) {
					base := plan
					if layer.request {
						base = request
					}
					changed := independentPrivateEdit(t, base, layer.path, func(object map[string]json.RawMessage) []byte {
						original, ok := object[field]
						if !ok {
							t.Fatal("independent required seed field missing")
						}
						alias := strings.ToUpper(field)
						if alias == field {
							alias = strings.ToLower(field)
						}
						switch kind {
						case "case":
							delete(object, field)
							object[alias] = original
						case "alias-pair":
							object[alias] = original
						case "missing":
							delete(object, field)
						case "null":
							object[field] = json.RawMessage("null")
						case "unknown":
							object["unexpected_private_member"] = json.RawMessage("true")
						}
						out, e := json.Marshal(object)
						if e != nil {
							t.Fatal("independent mutation encoding failed")
						}
						if kind == "exact-duplicate" {
							key, _ := json.Marshal(field)
							out = append(out[:len(out)-1], ',')
							out = append(out, key...)
							out = append(out, ':')
							out = append(out, original...)
							out = append(out, '}')
						}
						return out
					})
					req, body := request, plan
					if layer.request {
						req = changed
					} else {
						body = changed
					}
					if scan(req, body) == nil {
						t.Fatal("malformed persisted private shape accepted")
					}
				})
			}
		}
	}
}

func TestIndependentTaskPrivateRawUnicode(t *testing.T) {
	for _, raw := range []string{`{"value":"\ud83d\ude00"}`, `{"value":"\\uD800"}`, `{"value":"escaped quote: \"; literal slash: \\"}`} {
		if _, err := taskPrivateObject([]byte(raw), len(raw), []string{"value"}, nil); err != nil {
			t.Fatal("legal private Unicode or exact raw cap rejected")
		}
		if _, err := taskPrivateObject([]byte(raw+" "), len(raw), []string{"value"}, nil); err == nil {
			t.Fatal("complete visible raw cap ignored")
		}
	}
	for name, raw := range map[string]string{
		"high-surrogate":            `{"value":"\uD800"}`,
		"low-surrogate":             `{"value":"\udfff"}`,
		"high-then-scalar":          `{"value":"\ud800\u0061"}`,
		"high-then-escaped-literal": `{"value":"\ud800\\udc00"}`,
		"bad-utf8":                  "{\"value\":\"\xff\"}",
		"raw-control":               "{\"value\":\"\x01\"}",
		"escaped-key-duplicate":     `{"value":"x","v\u0061lue":"x"}`,
		"trailing-object":           `{"value":"x"}{}`,
		"trailing-scalar":           `{"value":"x"}true`,
		"null-object":               `null`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := taskPrivateObject([]byte(raw), 512, []string{"value"}, nil); err == nil {
				t.Fatal("invalid raw private JSON accepted")
			}
		})
	}
}
