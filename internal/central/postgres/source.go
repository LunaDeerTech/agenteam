package postgres

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type MigrationMode string

const (
	Transactional    MigrationMode = "tx"
	NonTransactional MigrationMode = "non_tx"
)

type MigrationEntry struct {
	Version  int64
	Filename string
	Checksum foundation.Digest
	Mode     MigrationMode
}
type RecoveryPlan struct {
	RestoreSteps   []string
	VerifyRestored string
}

// Source snapshots trusted, compiled SQL and recovery plans. No production CLI
// accepts an FS, SQL text or an arbitrary repair plan.
type Source struct{ data func() sourceData }
type sourceData struct {
	files   memoryFS
	entries []MigrationEntry
	plans   map[int64]RecoveryPlan
}

func EmbeddedSource() (Source, error) { return NewSource(migrations.SQL, nil) }

var migrationName = regexp.MustCompile(`^([0-9]{5})_[a-z][a-z0-9_]*\.sql$`)

func NewSource(source fs.FS, plans map[int64]RecoveryPlan) (Source, error) {
	if source == nil {
		return Source{}, failure(MigrationInvalid, nil)
	}
	files, err := fs.ReadDir(source, ".")
	if err != nil {
		return Source{}, failure(MigrationInvalid, err)
	}
	data := sourceData{files: make(memoryFS), plans: make(map[int64]RecoveryPlan)}
	for _, file := range files {
		match := migrationName.FindStringSubmatch(file.Name())
		if file.IsDir() || match == nil {
			return Source{}, failure(MigrationInvalid, nil)
		}
		version, _ := strconv.ParseInt(match[1], 10, 64)
		body, err := fs.ReadFile(source, file.Name())
		if err != nil {
			return Source{}, failure(MigrationInvalid, err)
		}
		mode := MigrationMode("")
		for _, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(line, "-- agenteam:transaction ") {
				if mode != "" {
					return Source{}, failure(MigrationInvalid, nil)
				}
				mode = MigrationMode(strings.TrimPrefix(line, "-- agenteam:transaction "))
			}
		}
		if mode != Transactional && mode != NonTransactional || !bytes.Contains(body, []byte("-- +goose Up\n")) || bytes.Contains(body, []byte("-- +goose Down")) {
			return Source{}, failure(MigrationInvalid, nil)
		}
		if bytes.Contains(body, []byte("-- +goose NO TRANSACTION")) != (mode == NonTransactional) {
			return Source{}, failure(MigrationInvalid, nil)
		}
		sum := sha256.Sum256(body)
		data.entries = append(data.entries, MigrationEntry{version, file.Name(), foundation.Digest("sha256:" + hex.EncodeToString(sum[:])), mode})
		data.files[file.Name()] = append([]byte(nil), body...)
		plan, exists := plans[version]
		if mode == NonTransactional {
			if !exists || len(plan.RestoreSteps) == 0 || strings.TrimSpace(plan.VerifyRestored) == "" {
				return Source{}, failure(MigrationInvalid, nil)
			}
			for _, step := range plan.RestoreSteps {
				if strings.TrimSpace(step) == "" || transactionControl(step) {
					return Source{}, failure(MigrationInvalid, nil)
				}
			}
			data.plans[version] = RecoveryPlan{append([]string(nil), plan.RestoreSteps...), plan.VerifyRestored}
		} else if exists {
			return Source{}, failure(MigrationInvalid, nil)
		}
	}
	if len(data.entries) == 0 {
		return Source{}, failure(MigrationInvalid, nil)
	}
	sort.Slice(data.entries, func(i, j int) bool { return data.entries[i].Version < data.entries[j].Version })
	for i, entry := range data.entries {
		if entry.Version != int64(i+1) {
			return Source{}, failure(MigrationInvalid, nil)
		}
	}
	if len(data.plans) != len(plans) {
		return Source{}, failure(MigrationInvalid, nil)
	}
	return Source{data: func() sourceData { return data }}, nil
}
func (s Source) Manifest() []MigrationEntry {
	if s.data == nil {
		return nil
	}
	return append([]MigrationEntry(nil), s.data().entries...)
}
func (s Source) Target() int64 {
	if s.data == nil {
		return 0
	}
	return int64(len(s.data().entries))
}
func (s Source) entry(version int64) (MigrationEntry, bool) {
	if version < 1 || version > s.Target() {
		return MigrationEntry{}, false
	}
	return s.data().entries[version-1], true
}

type memoryFS map[string][]byte

func (m memoryFS) Open(name string) (fs.File, error) {
	data, ok := m[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &memoryFile{Reader: bytes.NewReader(data), info: memoryInfo{name, int64(len(data))}}, nil
}
func (m memoryFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, fs.ErrNotExist
	}
	entries := make([]fs.DirEntry, 0, len(m))
	for name, data := range m {
		entries = append(entries, fs.FileInfoToDirEntry(memoryInfo{name, int64(len(data))}))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

type memoryFile struct {
	*bytes.Reader
	info memoryInfo
}

func (f *memoryFile) Close() error               { return nil }
func (f *memoryFile) Stat() (fs.FileInfo, error) { return f.info, nil }

type memoryInfo struct {
	name string
	size int64
}

func (m memoryInfo) Name() string       { return m.name }
func (m memoryInfo) Size() int64        { return m.size }
func (m memoryInfo) Mode() fs.FileMode  { return 0444 }
func (m memoryInfo) ModTime() time.Time { return time.Time{} }
func (m memoryInfo) IsDir() bool        { return false }
func (m memoryInfo) Sys() any           { return nil }
