package knowledge

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func newID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, err := f.NewID[T]()
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func queryFixture(t *testing.T) (cursor.Keyring, id.Actor, queryInput) {
	t.Helper()
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))) + `"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := id.NewHuman(newID[id.User](t), newID[id.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	return keys, actor, queryInput{kind: "children", project: newID[id.Project](t), filter: kc.ListFilter{TitleQuery: "a_%!"}}
}

func TestTitleCursorBindsOwnerScopeQueryAndPreservesText(t *testing.T) {
	keys, actor, q := queryFixture(t)
	binding, err := q.binding(actor)
	if err != nil {
		t.Fatal(err)
	}
	doc := kc.DocumentRef{ID: newID[kc.Document](t), Title: "e\u0301 文_100%"}
	token, err := titleCursor(keys, binding, doc)
	if err != nil {
		t.Fatal(err)
	}
	title, key, err := titleAfter(keys, token, binding)
	if err != nil || title != doc.Title || key != doc.ID.String() {
		t.Fatal("lost exact title/identity", err)
	}
	otherOwner, err := id.NewHuman(newID[id.User](t), newID[id.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	wrongOwner, _ := q.binding(otherOwner)
	if _, _, err = titleAfter(keys, token, wrongOwner); err == nil {
		t.Fatal("cursor crossed owner")
	}
	for name, change := range map[string]func(*queryInput){
		"project":       func(v *queryInput) { v.project = newID[id.Project](t) },
		"kind":          func(v *queryInput) { v.kind = "documents" },
		"parent":        func(v *queryInput) { x := newID[kc.Document](t); v.parent = &x },
		"literal query": func(v *queryInput) { v.filter.TitleQuery = "aX" },
		"source":        func(v *queryInput) { x := kc.File; v.filter.SourceKind = &x },
	} {
		t.Run(name, func(t *testing.T) {
			altered := q
			change(&altered)
			wrong, err := altered.binding(actor)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = titleAfter(keys, token, wrong); err == nil {
				t.Fatal("cursor binding ignored")
			}
		})
	}
	// Session identity is deliberately not a query key: a renewed current
	// session of the same owner may continue, after fresh Owner authorization.
	user, err := f.ParseID[id.User](actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := id.NewHuman(user, newID[id.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	sameOwner, _ := q.binding(renewed)
	if _, _, err = titleAfter(keys, token, sameOwner); err != nil {
		t.Fatal("cursor tied to session", err)
	}
}

func TestSignedWrongTitlePositionAndLiteralSearch(t *testing.T) {
	keys, actor, q := queryFixture(t)
	binding, _ := q.binding(actor)
	text, _ := cursor.Text("ordinary")
	key, _ := cursor.UUID(newID[kc.Document](t).String())
	generation := int64(1)
	for _, position := range []cursor.Position{
		{Scalars: []cursor.Scalar{cursor.Integer(1), key}},
		{Scalars: []cursor.Scalar{text}},
		{Scalars: []cursor.Scalar{text, key}, OrderGeneration: &generation},
	} {
		token, err := keys.Sign(binding, position)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = titleAfter(keys, token, binding); err == nil {
			t.Fatal("signed wrong position accepted")
		}
	}
	if got := literalTitle("100%_!x"); got != "100!%!_!!x" {
		t.Fatalf("not a literal substring: %q", got)
	}
	if got := literalTitle("一'é"); got != "一'é" {
		t.Fatal("changed literal title bytes")
	}
}
