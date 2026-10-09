// Offline only: production embedded migration source and metadata validation.
package main

import (
	"bytes"
	"fmt"
	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"io/fs"
	"testing/fstest"
)

func copySource() fstest.MapFS {
	result := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.SQL, ".")
	if err != nil {
		panic("source read")
	}
	for _, entry := range entries {
		raw, err := fs.ReadFile(migrations.SQL, entry.Name())
		if err != nil {
			panic("source body")
		}
		result[entry.Name()] = &fstest.MapFile{Data: raw}
	}
	return result
}
func main() {
	source, err := postgres.EmbeddedSource()
	if err != nil || source.Target() != 29 {
		panic("embedded target")
	}
	for i, entry := range source.Manifest() {
		if entry.Version != int64(i+1) || entry.Mode != postgres.Transactional {
			panic("continuous tx prefix")
		}
	}
	last := source.Manifest()[28]
	if last.Filename != "00029_project_variable_receipts.sql" {
		panic("exact migration")
	}
	prefix := copySource()
	delete(prefix, last.Filename)
	prior, err := postgres.NewSource(prefix, nil)
	if err != nil || prior.Target() != 28 {
		panic("prefix 28")
	}
	changes := []func(fstest.MapFS){
		func(m fstest.MapFS) { delete(m, "00028_cleanup_indexes.sql") },
		func(m fstest.MapFS) { m["00029_duplicate.sql"] = &fstest.MapFile{Data: m[last.Filename].Data} },
		func(m fstest.MapFS) {
			m["00030_project_variable_receipts.sql"] = m[last.Filename]
			delete(m, last.Filename)
		},
		func(m fstest.MapFS) {
			m[last.Filename].Data = bytes.Replace(m[last.Filename].Data, []byte("-- agenteam:transaction tx"), []byte("-- agenteam:transaction non_tx\n-- +goose NO TRANSACTION"), 1)
		},
		func(m fstest.MapFS) {
			m[last.Filename].Data = append(m[last.Filename].Data, []byte("\n-- +goose Down\n")...)
		},
		func(m fstest.MapFS) {
			m[last.Filename].Data = bytes.Replace(m[last.Filename].Data, []byte("-- +goose Up\n"), nil, 1)
		},
	}
	for i, change := range changes {
		candidate := copySource()
		change(candidate)
		if _, err := postgres.NewSource(candidate, nil); err == nil {
			panic(fmt.Sprintf("mutation %d accepted", i))
		}
	}
	fmt.Printf("actual source: prefix28/target29 tx PASS; %d metadata negatives PASS; no SQL execution\n", len(changes))
}
