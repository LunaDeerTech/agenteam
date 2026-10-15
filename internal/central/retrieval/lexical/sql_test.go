package lexical

import "testing"

func TestSQLUsesClosedAliasesAndBoundQuery(t *testing.T) {
	v, err := Expressions(PGroonga, MemoryProjection, 7)
	if err != nil || v.Match != "p.index_text &@~ $7" || v.Score != "pgroonga_score(p.tableoid,p.ctid)" {
		t.Fatal("wrong native extension expressions")
	}
	for _, relation := range []Relation{"", "p;DROP TABLE x", "c.index_text"} {
		if _, err := Expressions(PGroonga, relation, 1); err == nil {
			t.Fatal("unbound relation accepted")
		}
	}
	if _, err := Expressions(Backend("unselected"), KnowledgeChunks, 1); err == nil {
		t.Fatal("unknown backend silently replaced")
	}
}

func TestPGSearchUsesBoundQueryAndDomainRelation(t *testing.T) {
	for _, test := range []struct {
		relation     Relation
		parameter    int
		match, score string
	}{
		{KnowledgeChunks, 6, "c.id @@@ paradedb.match('index_text',$6,conjunction_mode=>false)", "paradedb.score(c.id)"},
		{MemoryProjection, 4, "p.memory_id @@@ paradedb.match('index_text',$4,conjunction_mode=>false)", "paradedb.score(p.memory_id)"},
	} {
		got, err := Expressions(PGSearch, test.relation, test.parameter)
		if err != nil || got.Match != test.match || got.Score != test.score {
			t.Fatalf("relation %q: expressions=%+v error=%v", test.relation, got, err)
		}
	}
	for _, relation := range []Relation{"", "p;DROP TABLE x", "c.index_text"} {
		if _, err := Expressions(PGSearch, relation, 1); err == nil {
			t.Fatal("unbound relation accepted")
		}
	}
	for _, parameter := range []int{-1, 0, 65} {
		if _, err := Expressions(PGSearch, KnowledgeChunks, parameter); err == nil {
			t.Fatal("invalid query parameter accepted")
		}
	}
}
