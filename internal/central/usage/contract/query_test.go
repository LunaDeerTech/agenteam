package contract

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestQueryLimitsRangeAndNoImplicitSystemScope(t *testing.T) {
	q := Query{Filter: Filter{ProjectID: fresh[id.Project](t)}, Limit: 50}
	must(t, q.Validate())
	q.Limit = 100
	must(t, q.Validate())
	q.Limit = 0
	reject(t, q.Validate())
	q.Limit = 101
	reject(t, q.Validate())
	q.Limit = 50
	q.Filter.From = ptr(instant(t))
	reject(t, q.Validate())
	q.Filter.To = ptr(instant(t))
	reject(t, q.Validate())
	end, e := f.NewInstant(q.Filter.From.Time().Add(time.Second))
	must(t, e)
	q.Filter.To = &end
	must(t, q.Validate())
	c := q.Clone()
	*c.Filter.To = *c.Filter.From
	reject(t, c.Validate())
	must(t, q.Validate())
	q.Filter.ProjectID = id.ProjectID{}
	reject(t, q.Validate())
}
func TestNullableGroupsAndConsistentAsOf(t *testing.T) {
	for _, by := range []GroupBy{ByAgent, ByExecution, ByMeeting} {
		must(t, (GroupKey{By: by}).Validate())
		reject(t, (GroupKey{By: by, ID: ptr("")}).Validate())
	}
	reject(t, (GroupKey{By: ByModel}).Validate())
	must(t, (GroupKey{By: ByModel, ID: ptr(fresh[struct{}](t).String())}).Validate())
	must(t, (GroupKey{By: ByDay, Day: ptr("2026-10-05")}).Validate())
	reject(t, (GroupKey{By: ByDay, Day: ptr("2026-02-30")}).Validate())
	reject(t, (GroupKey{By: ByDay, Day: ptr("2026-10-05"), ID: ptr("extra")}).Validate())
	p := AggregatePage{AsOf: instant(t), Items: []GroupSummary{{Key: GroupKey{By: ByAgent}, Summary: emptySummary(t)}}}
	must(t, p.Validate())
	c := p.Clone()
	later, e := f.NewInstant(p.AsOf.Time().Add(time.Second))
	must(t, e)
	c.Items[0].Summary.AsOf = later
	reject(t, c.Validate())
	c = p.Clone()
	*c.Items[0].Summary.Input.Sum = 1
	reject(t, c.Validate())
	must(t, p.Validate())
	q := AggregateQuery{Filter: Filter{ProjectID: fresh[id.Project](t)}, GroupBy: "system", Limit: 20}
	reject(t, q.Validate())
}
func TestUsageQueryAndProjectionLogsAreSafe(t *testing.T) {
	const canary = "private-query-canary"
	r := invocation(t)
	r.Identity.ModelName = canary
	for _, v := range []any{r, Query{Cursor: canary}, Page{Items: []Invocation{r}, NextCursor: canary}, AggregateQuery{Cursor: canary}} {
		if strings.Contains(fmt.Sprintf("%#v", v), canary) {
			t.Fatalf("format leaked %T", v)
		}
		var b bytes.Buffer
		slog.New(slog.NewJSONHandler(&b, nil)).Info("test", "value", v)
		if strings.Contains(b.String(), canary) {
			t.Fatalf("slog leaked %T", v)
		}
	}
	p := Page{Items: []Invocation{r}}
	must(t, p.Validate())
	c := p.Clone()
	c.Items[0].Final.Status = Failed
	if p.Items[0].Final.Status != Succeeded {
		t.Fatal("page alias")
	}
	p.Items = make([]Invocation, 101)
	reject(t, p.Validate())
}
