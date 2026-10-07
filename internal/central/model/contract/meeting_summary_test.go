package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func summaryRequest(t *testing.T) UpdateMeetingSummarySelectionRequest {
	t.Helper()
	actor, err := id.NewHuman(fresh[id.User](t), fresh[id.Session](t))
	must(t, err)
	return UpdateMeetingSummarySelectionRequest{
		CommandMeta:     CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "summary-key-SENTINEL"},
		SelectionID:     fresh[struct{}](t).String(),
		ExpectedVersion: 1,
		Model:           fresh[Model](t),
	}
}

func summaryInvalid(t *testing.T, err error) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.InvalidArgument || fault.CommitState != f.NotStarted {
		t.Fatalf("want INVALID_ARGUMENT/not_started, got %v", err)
	}
}

func TestMeetingSummarySelectionValidation(t *testing.T) {
	valid := MeetingSummarySelection{ID: "018f0000-0000-7000-8000-00000000000a", Version: 1}
	must(t, valid.Validate())
	valid.Model = ptr(fresh[Model](t))
	must(t, valid.Validate())
	for _, tc := range []struct {
		name string
		edit func(*MeetingSummarySelection)
	}{
		{"empty-id", func(s *MeetingSummarySelection) { s.ID = "" }},
		{"uppercase-id", func(s *MeetingSummarySelection) { s.ID = strings.ToUpper(s.ID) }},
		{"non-v7-id", func(s *MeetingSummarySelection) { s.ID = "018f0000-0000-4000-8000-00000000000a" }},
		{"invalid-variant", func(s *MeetingSummarySelection) { s.ID = "018f0000-0000-7000-0000-00000000000a" }},
		{"id-space", func(s *MeetingSummarySelection) { s.ID += " " }},
		{"zero-version", func(s *MeetingSummarySelection) { s.Version = 0 }},
		{"negative-version", func(s *MeetingSummarySelection) { s.Version = -1 }},
		{"invalid-present-model", func(s *MeetingSummarySelection) { s.Model = new(ModelID) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := valid.Clone()
			tc.edit(&s)
			summaryInvalid(t, s.Validate())
		})
	}
}

func TestMeetingSummarySelectionCloneAndWirePrecision(t *testing.T) {
	key := fresh[struct{}](t).String()
	model := fresh[Model](t)
	for _, version := range []f.Version{1, 9007199254740993, math.MaxInt64} {
		for _, selected := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/selected=%t", version.String(), selected), func(t *testing.T) {
				s := MeetingSummarySelection{ID: key, Version: version}
				modelJSON := "null"
				if selected {
					s.Model = ptr(model)
					modelJSON = fmt.Sprintf("%q", model.String())
				}
				must(t, s.Validate())
				raw, err := json.Marshal(s)
				must(t, err)
				want := fmt.Sprintf(`{"id":%q,"version":%q,"model":%s}`, key, version.String(), modelJSON)
				if string(raw) != want {
					t.Fatalf("wire = %s; want %s", raw, want)
				}
				var roundtrip MeetingSummarySelection
				must(t, json.Unmarshal(raw, &roundtrip))
				must(t, roundtrip.Validate())
				if roundtrip.ID != key || roundtrip.Version != version || (roundtrip.Model != nil) != selected || selected && *roundtrip.Model != model {
					t.Fatal("roundtrip changed selection")
				}
				clone := s.Clone()
				if !selected {
					if clone.Model != nil {
						t.Fatal("clone invented a selected model")
					}
					return
				}
				if clone.Model == s.Model || *clone.Model != model {
					t.Fatal("clone did not copy the model identity")
				}
				*clone.Model = fresh[Model](t)
				if *s.Model != model {
					t.Fatal("clone mutation changed source")
				}
				cloneValue := *clone.Model
				*s.Model = fresh[Model](t)
				if *clone.Model != cloneValue {
					t.Fatal("source mutation changed clone")
				}
			})
		}
	}
}

func TestMeetingSummaryRequestRequiresHumanSystemAndModel(t *testing.T) {
	valid := summaryRequest(t)
	must(t, valid.Validate())
	project := fresh[id.Project](t)
	projectScope, err := id.InProject(project)
	must(t, err)
	memoryScope, err := id.InAgentMemory(project, fresh[id.Agent](t))
	must(t, err)
	agent, err := id.NewAgentRun(project, fresh[id.Agent](t), fresh[id.Execution](t))
	must(t, err)
	registration, err := id.RegisterService(id.ModelRuntime)
	must(t, err)
	service, err := registration.Actor(fresh[struct{}](t).String(), id.SystemScope())
	must(t, err)
	for _, tc := range []struct {
		name string
		edit func(*UpdateMeetingSummarySelectionRequest)
	}{
		{"zero-actor", func(r *UpdateMeetingSummarySelectionRequest) { r.Actor = id.Actor{} }},
		{"agent", func(r *UpdateMeetingSummarySelectionRequest) { r.Actor = agent }},
		{"service", func(r *UpdateMeetingSummarySelectionRequest) { r.Actor = service }},
		{"zero-scope", func(r *UpdateMeetingSummarySelectionRequest) { r.Scope = id.Scope{} }},
		{"project-scope", func(r *UpdateMeetingSummarySelectionRequest) { r.Scope = projectScope }},
		{"memory-scope", func(r *UpdateMeetingSummarySelectionRequest) { r.Scope = memoryScope }},
		{"empty-key", func(r *UpdateMeetingSummarySelectionRequest) { r.Key = "" }},
		{"invalid-key", func(r *UpdateMeetingSummarySelectionRequest) { r.Key = "invalid key" }},
		{"long-key", func(r *UpdateMeetingSummarySelectionRequest) { r.Key = f.IdempotencyKey(strings.Repeat("a", 129)) }},
		{"empty-selection", func(r *UpdateMeetingSummarySelectionRequest) { r.SelectionID = "" }},
		{"noncanonical-selection", func(r *UpdateMeetingSummarySelectionRequest) { r.SelectionID = "018F0000-0000-7000-8000-00000000000A" }},
		{"non-v7-selection", func(r *UpdateMeetingSummarySelectionRequest) { r.SelectionID = "018f0000-0000-4000-8000-00000000000a" }},
		{"zero-version", func(r *UpdateMeetingSummarySelectionRequest) { r.ExpectedVersion = 0 }},
		{"negative-version", func(r *UpdateMeetingSummarySelectionRequest) { r.ExpectedVersion = -1 }},
		{"clear-model", func(r *UpdateMeetingSummarySelectionRequest) { r.Model = ModelID{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			tc.edit(&r)
			summaryInvalid(t, r.Validate())
		})
	}
	valid.ExpectedVersion = math.MaxInt64
	must(t, valid.Validate()) // Version exhaustion is a service mutation rule.
	raw, err := json.Marshal(valid)
	must(t, err)
	var fields map[string]json.RawMessage
	must(t, json.Unmarshal(raw, &fields))
	if len(fields) != 4 || string(fields["scope"]) != `{"kind":"system"}` || string(fields["expected_version"]) != `"9223372036854775807"` || string(fields["selection_id"]) != fmt.Sprintf("%q", valid.SelectionID) || string(fields["model"]) != fmt.Sprintf("%q", valid.Model.String()) {
		t.Fatalf("unexpected request JSON: %s", raw)
	}
}

func TestMeetingSummarySafeFormatting(t *testing.T) {
	r := summaryRequest(t)
	s := MeetingSummarySelection{ID: r.SelectionID, Version: r.ExpectedVersion, Model: ptr(r.Model)}
	for _, tc := range []struct {
		name string
		v    any
		want string
	}{
		{"selection", s, "model_meeting_summary_selection"},
		{"request", r, "model_update_meeting_summary_selection_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
				if got := fmt.Sprintf(format, tc.v); got != tc.want {
					t.Fatalf("unsafe %s formatting: %s", format, got)
				}
			}
			if got := slog.AnyValue(tc.v).Resolve(); got.Kind() != slog.KindString || got.String() != tc.want {
				t.Fatal("unsafe structured log value")
			}
		})
	}
}

func TestMeetingSummaryDoesNotChangeExistingSelectionContracts(t *testing.T) {
	r := summaryRequest(t)
	s := PlatformSelection{ID: r.SelectionID, Version: 9007199254740993, Embedding: fresh[Model](t), Memory: r.Model}
	must(t, s.Validate())
	raw, err := json.Marshal(s)
	must(t, err)
	want := fmt.Sprintf(`{"id":%q,"version":"9007199254740993","embedding":%q,"memory":%q,"reranker":null,"image":null}`, s.ID, s.Embedding.String(), s.Memory.String())
	if string(raw) != want {
		t.Fatalf("legacy PlatformSelection changed: %s", raw)
	}
	must(t, (SelectionRef{Kind: "platform", Selector: MeetingSummarySelector}).Validate())
	project := fresh[id.Project](t)
	must(t, (SelectionRef{Kind: "project_summary", ProjectID: &project, Selector: MeetingSummarySelector}).Validate())
}
