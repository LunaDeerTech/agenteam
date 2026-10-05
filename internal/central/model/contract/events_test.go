package contract

import "testing"

func TestConfigurationEventsContainOnlyClosedSafeFields(t *testing.T) {
	x := setup(t)
	e := ConfigurationChanged{AggregateID: x.snapshot.Identity.ModelID.String(), Version: 1, Scope: "system", ChangedFields: []string{"capabilities", "parameters"}}
	must(t, e.Validate())
	c := e.Clone()
	c.ChangedFields[0] = "raw_prompt"
	reject(t, c.Validate())
	if e.ChangedFields[0] != "capabilities" {
		t.Fatal("alias")
	}
	c = e.Clone()
	c.ProjectID = x.c.ProjectID.String()
	reject(t, c.Validate())
	c.Scope = "project"
	must(t, c.Validate())
	c.ChangedFields = append(c.ChangedFields, "parameters")
	reject(t, c.Validate())
	s := EmbeddingSelectionChanged{AggregateID: fresh[struct{}](t).String(), Version: 1, NewModelID: x.snapshot.Identity.ModelID}
	must(t, s.Validate())
	s.OldModelID = &s.NewModelID
	reject(t, s.Validate())
	s.OldModelID = ptr(fresh[Model](t))
	must(t, s.Validate())
	d := s.Clone()
	*d.OldModelID = s.NewModelID
	if *s.OldModelID == s.NewModelID {
		t.Fatal("alias")
	}
}
