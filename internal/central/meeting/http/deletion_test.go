package meetinghttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

type deletionSpy struct {
	writes, lookups int
	original        c.DeleteRequest
	receipt         c.DeleteReceipt
	found           bool
	failure         error
}

func (s *deletionSpy) Delete(_ context.Context, _ i.Actor, r c.DeleteRequest) (c.DeleteReceipt, error) {
	s.writes++
	s.original = r
	return s.receipt, s.failure
}
func (s *deletionSpy) LookupDelete(_ context.Context, _ i.Actor, r c.DeleteRequest) (c.DeleteReceipt, bool, error) {
	s.lookups++
	s.original = r
	return s.receipt, s.found, nil
}

func deletionServer(t *testing.T) (*httptest.Server, *deletionSpy, *testBoundary, string) {
	t.Helper()
	actor, err := i.NewHuman(fresh[i.User](t), fresh[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	s := &deletionSpy{receipt: c.DeleteReceipt{DeletionID: fresh[c.Deletion](t), MeetingID: fresh[c.Meeting](t), AcceptedVersion: 9007199254740994, Phase: c.DeletionCancelling, RequestedAt: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}}
	b := &testBoundary{actor: actor}
	h := httpapi.WithRequestID(slog.New(slog.NewTextHandler(io.Discard, nil)), &deletionHandler{service: s, boundary: b})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, s, b, "/api/v1/projects/" + fresh[i.Project](t).String() + "/meetings/" + s.receipt.MeetingID.String() + "/deletion"
}

func TestMeetingDeletionHTTPKeepsOriginalCommandAndDistinctCompletion(t *testing.T) {
	srv, s, _, path := deletionServer(t)
	const body = `{"expected_version":"9007199254740993"}`
	if !HandlesDeletionPath(path) || !HandlesDeletionPath(path+"/lookup") || HandlesDeletionPath(path+"/lookup/extra") {
		t.Fatal("deletion route shape changed")
	}
	status, raw := request(t, srv, "POST", path, body)
	var receipt c.DeleteReceipt
	if status != 200 || json.Unmarshal(raw, &receipt) != nil || receipt.Validate() != nil || receipt.Phase != c.DeletionCancelling || receipt.DeletedAt != nil || s.writes != 1 || s.original.Validate() != nil || s.original.ExpectedVersion != 9007199254740993 || s.original.Meta.IdempotencyKey != "original-meeting-command" || s.original.MeetingID != s.receipt.MeetingID {
		t.Fatal("accepted deletion was not an exact cancelling receipt", status)
	}
	for _, found := range []bool{false, true} {
		s.found = found
		status, raw = request(t, srv, "POST", path+"/lookup", body)
		var result struct {
			Status  string           `json:"status"`
			Receipt *c.DeleteReceipt `json:"receipt"`
		}
		if status != 200 || json.Unmarshal(raw, &result) != nil || s.writes != 1 || s.original.Meta.IdempotencyKey != "original-meeting-command" {
			t.Fatal("lookup issued another deletion", status)
		}
		if found && (result.Status != "committed" || result.Receipt == nil || result.Receipt.Phase != c.DeletionCancelling) || !found && (result.Status != "not_found" || result.Receipt != nil) {
			t.Fatal("lookup confused command observation with completion")
		}
	}
	completed := s.receipt.RequestedAt.Add(time.Second)
	s.receipt.Phase, s.receipt.DeletedAt, s.receipt.Replayed = c.DeletionCompleted, &completed, true
	status, raw = request(t, srv, "POST", path+"/lookup", body)
	var result struct {
		Status  string          `json:"status"`
		Receipt c.DeleteReceipt `json:"receipt"`
	}
	if status != 200 || json.Unmarshal(raw, &result) != nil || result.Status != "committed" || result.Receipt.Phase != c.DeletionCompleted || result.Receipt.Validate() != nil || s.writes != 1 || s.lookups != 3 {
		t.Fatal("completed original deletion was not queryable", status)
	}
}

func TestMeetingDeletionHTTPRejectsUnconfirmedOrForeignInput(t *testing.T) {
	srv, s, b, path := deletionServer(t)
	for _, body := range []string{`{}`, `{"expected_version":null}`, `{"expected_version":1}`, `{"expected_version":"0"}`, `{"expected_version":"1","meeting_id":"other"}`, `{"expected_version":"1","force":true}`} {
		if status, _ := request(t, srv, "POST", path, body); status != 400 || s.writes != 0 {
			t.Fatal("malformed deletion reached the domain", status)
		}
	}
	for _, bad := range []struct{ method, path string }{{"GET", path}, {"POST", path + "?force=true"}, {"POST", path + "/"}} {
		if status, _ := request(t, srv, bad.method, bad.path, `{"expected_version":"1"}`); status < 400 || s.writes != 0 || s.lookups != 0 {
			t.Fatal("alternate deletion route reached domain", status)
		}
	}
	b.deny = true
	if status, _ := request(t, srv, "POST", "/api/v1/projects/invalid/meetings/invalid/deletion", `{}`); status != 403 || s.writes != 0 {
		t.Fatal("scope parsed before current Human boundary", status)
	}
}

func TestMeetingDeletionHTTPUnknownOnlyQueriesOriginalKey(t *testing.T) {
	srv, s, _, path := deletionServer(t)
	const body = `{"expected_version":"9007199254740993"}`
	s.failure = f.NewFault(f.CommitUnknown, f.Unknown)
	if status, _ := request(t, srv, "POST", path, body); status < 400 || s.writes != 1 {
		t.Fatal("unknown deletion fabricated a success", status)
	}
	s.failure, s.found = nil, true
	if status, _ := request(t, srv, "POST", path+"/lookup", body); status != 200 || s.writes != 1 || s.lookups != 1 || s.original.Meta.IdempotencyKey != "original-meeting-command" {
		t.Fatal("recovery resubmitted deletion", status)
	}
	s.receipt.MeetingID = fresh[c.Meeting](t)
	if status, _ := request(t, srv, "POST", path+"/lookup", body); status != 503 || s.writes != 1 {
		t.Fatal("foreign deletion receipt published", status)
	}
}
