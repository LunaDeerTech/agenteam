package contract

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type Filter struct {
	ProjectID    id.ProjectID     `json:"project_id"`
	ConsumerKind *mc.ConsumerKind `json:"consumer_kind,omitempty"`
	AgentID      *id.AgentID      `json:"agent_id,omitempty"`
	ExecutionID  *id.ExecutionID  `json:"execution_id,omitempty"`
	MeetingID    string           `json:"meeting_id,omitempty"`
	Purpose      *mc.Purpose      `json:"purpose,omitempty"`
	ProviderID   *mc.ProviderID   `json:"provider_id,omitempty"`
	ModelID      *mc.ModelID      `json:"model_id,omitempty"`
	Status       *TerminalStatus  `json:"status,omitempty"`
	From         *f.Instant       `json:"from,omitempty"`
	To           *f.Instant       `json:"to,omitempty"`
}

func (q Filter) Validate() error {
	if q.ProjectID.Validate() != nil || q.ConsumerKind != nil && !q.ConsumerKind.Valid() || q.AgentID != nil && q.AgentID.Validate() != nil || q.ExecutionID != nil && q.ExecutionID.Validate() != nil || q.MeetingID != "" && !uuid(q.MeetingID) || q.Purpose != nil && !q.Purpose.Valid() || q.ProviderID != nil && q.ProviderID.Validate() != nil || q.ModelID != nil && q.ModelID.Validate() != nil || q.Status != nil && !q.Status.Valid() || (q.From == nil) != (q.To == nil) {
		return bad()
	}
	if q.From != nil && (q.From.Validate() != nil || q.To.Validate() != nil || !q.From.Time().Before(q.To.Time())) {
		return bad()
	}
	return nil
}
func (q Filter) Clone() Filter {
	q.ConsumerKind = copyPtr(q.ConsumerKind)
	q.AgentID = copyPtr(q.AgentID)
	q.ExecutionID = copyPtr(q.ExecutionID)
	q.Purpose = copyPtr(q.Purpose)
	q.ProviderID = copyPtr(q.ProviderID)
	q.ModelID = copyPtr(q.ModelID)
	q.Status = copyPtr(q.Status)
	q.From = copyPtr(q.From)
	q.To = copyPtr(q.To)
	return q
}

type Query struct {
	Filter Filter `json:"filter"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit"`
}

func (q Query) Validate() error {
	if q.Filter.Validate() != nil || q.Limit < 1 || q.Limit > 100 || !utf8.ValidString(q.Cursor) {
		return bad()
	}
	return nil
}
func (q Query) Clone() Query { q.Filter = q.Filter.Clone(); return q }

type Page struct {
	Items      []Invocation `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

func (p Page) Validate() error {
	if len(p.Items) > 100 || !utf8.ValidString(p.NextCursor) {
		return bad()
	}
	for _, v := range p.Items {
		if v.Validate() != nil {
			return bad()
		}
	}
	return nil
}
func (p Page) Clone() Page {
	p.Items = append([]Invocation(nil), p.Items...)
	for n := range p.Items {
		p.Items[n] = p.Items[n].Clone()
	}
	return p
}

type GroupBy string

const (
	ByConsumer  GroupBy = "consumer"
	ByAgent     GroupBy = "agent"
	ByModel     GroupBy = "model"
	ByProvider  GroupBy = "provider"
	ByExecution GroupBy = "execution"
	ByMeeting   GroupBy = "meeting"
	ByPurpose   GroupBy = "purpose"
	ByDay       GroupBy = "day"
)

func (g GroupBy) Valid() bool {
	switch g {
	case ByConsumer, ByAgent, ByModel, ByProvider, ByExecution, ByMeeting, ByPurpose, ByDay:
		return true
	}
	return false
}

type AggregateQuery struct {
	Filter  Filter  `json:"filter"`
	GroupBy GroupBy `json:"group_by"`
	Cursor  string  `json:"cursor,omitempty"`
	Limit   int     `json:"limit"`
}

func (q AggregateQuery) Validate() error {
	if !q.GroupBy.Valid() {
		return bad()
	}
	return (Query{q.Filter, q.Cursor, q.Limit}).Validate()
}
func (q AggregateQuery) Clone() AggregateQuery { q.Filter = q.Filter.Clone(); return q }

type GroupKey struct {
	By  GroupBy `json:"by"`
	ID  *string `json:"id"`
	Day *string `json:"day"`
}

func (k GroupKey) Validate() error {
	if !k.By.Valid() {
		return bad()
	}
	if k.By == ByDay {
		if k.ID != nil || k.Day == nil {
			return bad()
		}
		d, e := time.Parse("2006-01-02", *k.Day)
		if e != nil || d.Format("2006-01-02") != *k.Day {
			return bad()
		}
		return nil
	}
	if k.Day != nil {
		return bad()
	}
	if k.ID == nil {
		if k.By == ByAgent || k.By == ByExecution || k.By == ByMeeting {
			return nil
		}
		return bad()
	}
	switch k.By {
	case ByConsumer:
		if !mc.ConsumerKind(*k.ID).Valid() {
			return bad()
		}
	case ByPurpose:
		if !mc.Purpose(*k.ID).Valid() {
			return bad()
		}
	default:
		if !uuid(*k.ID) {
			return bad()
		}
	}
	return nil
}
func (k GroupKey) Clone() GroupKey { k.ID = copyPtr(k.ID); k.Day = copyPtr(k.Day); return k }

type GroupSummary struct {
	Key     GroupKey `json:"key"`
	Summary Summary  `json:"summary"`
}

func (g GroupSummary) Validate() error {
	if g.Key.Validate() != nil || g.Summary.Validate() != nil {
		return bad()
	}
	return nil
}
func (g GroupSummary) Clone() GroupSummary {
	g.Key = g.Key.Clone()
	g.Summary = g.Summary.Clone()
	return g
}

type AggregatePage struct {
	Items      []GroupSummary `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	AsOf       f.Instant      `json:"as_of"`
}

func (p AggregatePage) Validate() error {
	if len(p.Items) > 100 || !utf8.ValidString(p.NextCursor) || p.AsOf.Validate() != nil {
		return bad()
	}
	for _, v := range p.Items {
		if v.Validate() != nil || v.Summary.AsOf != p.AsOf {
			return bad()
		}
	}
	return nil
}
func (p AggregatePage) Clone() AggregatePage {
	p.Items = append([]GroupSummary(nil), p.Items...)
	for n := range p.Items {
		p.Items[n] = p.Items[n].Clone()
	}
	return p
}

type Reader interface {
	List(context.Context, id.Actor, Query) (Page, error)
	Aggregate(context.Context, id.Actor, AggregateQuery) (AggregatePage, error)
}

func uuid(s string) bool { v, e := f.ParseID[struct{}](s); return e == nil && v.String() == s }

func (Filter) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_filter")) }
func (Filter) LogValue() slog.Value       { return slog.StringValue("usage_filter") }

func (Query) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_query")) }
func (Query) LogValue() slog.Value       { return slog.StringValue("usage_query") }

func (Page) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_page")) }
func (Page) LogValue() slog.Value       { return slog.StringValue("usage_page") }

func (AggregateQuery) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_aggregate_query")) }
func (AggregateQuery) LogValue() slog.Value       { return slog.StringValue("usage_aggregate_query") }

func (GroupKey) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_group_key")) }
func (GroupKey) LogValue() slog.Value       { return slog.StringValue("usage_group_key") }

func (GroupSummary) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_group_summary")) }
func (GroupSummary) LogValue() slog.Value       { return slog.StringValue("usage_group_summary") }

func (AggregatePage) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_aggregate_page")) }
func (AggregatePage) LogValue() slog.Value       { return slog.StringValue("usage_aggregate_page") }
