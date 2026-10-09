package work

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestBlockerPageConstructionAndInput(t *testing.T) {
	store, deps := pureBlockerPorts(t)
	reader, err := NewBlockerReader(store, deps.Authority, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	var missing *denialStore
	for _, candidate := range []Store{nil, missing, &denialStore{}} {
		_, err = NewBlockerReader(candidate, deps.Authority, pureKeys(t))
		pureCode(t, err, f.DependencyUnbound)
	}
	_, err = NewBlockerReader(store, nil, pureKeys(t))
	pureCode(t, err, f.DependencyUnbound)
	_, err = NewBlockerReader(store, deps.Authority, cursor.Keyring{})
	pureCode(t, err, f.InvalidArgument)
	a, p, task := pureActor(t, 2), pureID[i.Project](t, 3), pureID[c.Task](t, 4)
	for _, limit := range []int{-1, 0, 201} {
		out, e := reader.ListTaskBlockersPage(context.Background(), a, p, task, c.TaskBlockersAll, f.PageRequest{Limit: limit})
		pureCode(t, e, f.InvalidArgument)
		if out.Items != nil || out.NextCursor != "" {
			t.Fatal("invalid input returned a page")
		}
	}
	_, err = reader.ListTaskBlockersPage(nil, a, p, task, c.TaskBlockersAll, f.DefaultPageRequest())
	pureCode(t, err, f.InvalidArgument)
	_, err = reader.ListTaskBlockersPage(context.Background(), a, p, task, "", f.DefaultPageRequest())
	pureCode(t, err, f.InvalidArgument)
	var unbound *BlockerReader
	_, err = unbound.ListTaskBlockersPage(context.Background(), a, p, task, c.TaskBlockersAll, f.DefaultPageRequest())
	pureCode(t, err, f.DependencyUnbound)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = reader.ListTaskBlockersPage(ctx, a, p, task, c.TaskBlockersAll, f.DefaultPageRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation cause lost")
	}
	if store.touches.Load() != 0 {
		t.Fatal("pure rejection accessed SQL")
	}
}

func TestBlockerPageCursorIdentityAndGeneration(t *testing.T) {
	keys := pureKeys(t)
	p, task, owner := pureID[i.Project](t, 11), pureID[c.Task](t, 12), pureID[i.User](t, 13).String()
	binding, err := blockerPageBinding(p, task, owner, c.TaskBlockersAll)
	if err != nil {
		t.Fatal(err)
	}
	at, _ := f.ParseInstant("2026-10-09T01:02:03.456789Z")
	v := c.TaskBlocker{ID: pureID[c.TaskBlockerIdentity](t, 14), CreatedAt: at}
	token, err := blockerPageToken(keys, binding, 17, v)
	if err != nil {
		t.Fatal(err)
	}
	after, err := blockerPageAfter(keys, token, binding, 17)
	if err != nil || after == nil || after.id != v.ID || after.at.String() != "2026-10-09T01:02:03.456789Z" {
		t.Fatal("lost exact keyset position", err)
	}
	_, err = blockerPageAfter(keys, token, binding, 18)
	pureCode(t, err, f.CursorStale)
	_, err = blockerPageAfter(keys, token, binding, 16)
	pureCode(t, err, f.CursorStale)
	for _, changed := range []struct {
		project c.ProjectID
		task    c.TaskID
		owner   string
		status  c.TaskBlockerStatus
	}{
		{pureID[i.Project](t, 21), task, owner, c.TaskBlockersAll},
		{p, pureID[c.Task](t, 22), owner, c.TaskBlockersAll},
		{p, task, pureID[i.User](t, 23).String(), c.TaskBlockersAll},
		{p, task, owner, c.TaskBlockersResolved},
		{p, task, owner, c.TaskBlockersUnresolved},
	} {
		b, e := blockerPageBinding(changed.project, changed.task, changed.owner, changed.status)
		if e != nil {
			t.Fatal(e)
		}
		_, e = blockerPageAfter(keys, token, b, 17)
		pureCode(t, e, f.CursorInvalid)
	}
	wrongOrder := binding
	wrongOrder.Order = "created_at:desc,id:desc"
	_, err = blockerPageAfter(keys, token, wrongOrder, 17)
	pureCode(t, err, f.CursorInvalid)
	for _, bad := range []string{token + ".", " " + token, strings.Repeat("x", cursor.MaxTokenBytes+1)} {
		_, err = blockerPageAfter(keys, bad, binding, 17)
		pureCode(t, err, f.CursorInvalid)
	}
	if after, e := blockerPageAfter(keys, "", binding, 17); e != nil || after != nil {
		t.Fatal("first page has a position")
	}
}

func TestBlockerPageCursorRejectsSignedWrongPosition(t *testing.T) {
	keys := pureKeys(t)
	binding, err := blockerPageBinding(pureID[i.Project](t, 1), pureID[c.Task](t, 2), pureID[i.User](t, 3).String(), c.TaskBlockersAll)
	if err != nil {
		t.Fatal(err)
	}
	at, _ := f.ParseInstant("2026-10-09T00:00:00.123456Z")
	instant, _ := cursor.Instant(at)
	id, _ := cursor.UUID(pureID[c.TaskBlockerIdentity](t, 4).String())
	text, _ := cursor.Text(at.String())
	generation := int64(2)
	for _, position := range []cursor.Position{
		{Scalars: []cursor.Scalar{instant, id}},
		{Scalars: []cursor.Scalar{instant}, OrderGeneration: &generation},
		{Scalars: []cursor.Scalar{instant, id, id}, OrderGeneration: &generation},
		{Scalars: []cursor.Scalar{text, id}, OrderGeneration: &generation},
		{Scalars: []cursor.Scalar{instant, cursor.Integer(4)}, OrderGeneration: &generation},
		{Scalars: []cursor.Scalar{id, instant}, OrderGeneration: &generation},
	} {
		token, e := keys.Sign(binding, position)
		if e != nil {
			t.Fatal(e)
		}
		_, e = blockerPageAfter(keys, token, binding, 3)
		pureCode(t, e, f.CursorInvalid) // malformed shape precedes stale generation
	}
}
