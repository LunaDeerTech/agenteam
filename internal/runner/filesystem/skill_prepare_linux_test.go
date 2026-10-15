//go:build linux

package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func skillRequest() SkillPrepareRequest {
	return SkillPrepareRequest{SkillID: "11111111-1111-7111-8111-111111111111", RevisionID: "22222222-2222-7222-8222-222222222222", ArchiveSHA256: "sha256:" + strings.Repeat("a", 64), Files: []SkillFile{{"SKILL.md", []byte("# Test skill\n")}, {"scripts/run.sh", []byte("printf 'prepared skill\\n'\n")}}}
}
func noSkillTemporary(t *testing.T, base string) {
	t.Helper()
	err := filepath.WalkDir(base, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(e.Name(), ".prepare-") {
			t.Error("temporary package remained")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestSkillPrepareRealPublishReuseAndConflict(t *testing.T) {
	base, _, root := fixture(t)
	request := skillRequest()
	out, err := root.PrepareSkill(context.Background(), request)
	if err != nil || out.State != WriteCommitted || !out.Durable || out.Reused {
		t.Fatal("initial package publication", out, err)
	}
	readWritten(t, root, out.RelativePath+"/scripts/run.sh", request.Files[1].Data)
	reused, err := root.PrepareSkill(context.Background(), request)
	if err != nil || !reused.Reused || !reused.Durable || reused.RelativePath != out.RelativePath {
		t.Fatal("exact package reuse", err)
	}
	file := filepath.Join(base, out.RelativePath, "scripts/run.sh")
	if err = os.Chmod(file, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := root.PrepareSkill(context.Background(), request); err == nil || got.State == WriteCommitted {
		t.Fatal("tampered existing package accepted")
	}
	body, err := os.ReadFile(file)
	if err != nil || string(body) != "tampered" {
		t.Fatal("existing user changes overwritten")
	}
	noSkillTemporary(t, base)
}
func TestSkillPrepareRejectsLinkAndForeignContents(t *testing.T) {
	for _, at := range []string{"managed-parent", "existing-file", "extra-file"} {
		t.Run(at, func(t *testing.T) {
			base, _, root := fixture(t)
			request := skillRequest()
			outside := t.TempDir()
			sentinel := filepath.Join(outside, "sentinel")
			if err := os.WriteFile(sentinel, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			if at == "managed-parent" {
				if err := os.Symlink(outside, filepath.Join(base, skillDirectory)); err != nil {
					t.Fatal(err)
				}
			} else {
				got, err := root.PrepareSkill(context.Background(), request)
				if err != nil {
					t.Fatal(err)
				}
				if at == "existing-file" {
					target := filepath.Join(base, got.RelativePath, "SKILL.md")
					if err = os.Remove(target); err != nil {
						t.Fatal(err)
					}
					if err = os.Symlink(sentinel, target); err != nil {
						t.Fatal(err)
					}
				} else {
					if err = os.WriteFile(filepath.Join(base, got.RelativePath, "foreign"), []byte("keep"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if got, err := root.PrepareSkill(context.Background(), request); err == nil || got.State == WriteCommitted {
				t.Fatal("link or foreign package accepted")
			}
			body, err := os.ReadFile(sentinel)
			if err != nil || string(body) != "untouched" {
				t.Fatal("escaped root")
			}
		})
	}
}

type controlledSkillRoot struct {
	nativeRoot
	prepare func(context.Context, SkillPrepareRequest, []byte) (SkillPrepareResult, error)
}

func (r controlledSkillRoot) prepareSkill(ctx context.Context, q SkillPrepareRequest, p []byte) (SkillPrepareResult, error) {
	return r.prepare(ctx, q, p)
}
func TestSkillPrepareCancellationJoinsAndRemovesOwnedTemporary(t *testing.T) {
	base, _, root := fixture(t)
	native := root.fs.(*linuxRoot)
	entered, release := make(chan struct{}), make(chan struct{})
	root.fs = controlledSkillRoot{native, func(ctx context.Context, q SkillPrepareRequest, p []byte) (SkillPrepareResult, error) {
		return native.prepareSkillWith(ctx, q, p, func() { close(entered); <-release })
	}}
	returned := make(chan error, 1)
	go func() {
		v, e := root.PrepareSkill(context.Background(), skillRequest())
		if v.State != WriteNotCommitted {
			returned <- errors.New("cancelled package published")
			return
		}
		returned <- e
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("publication barrier not entered")
	}
	root.Stop()
	drained := make(chan error, 1)
	go func() { drained <- root.Drain(context.Background()) }()
	select {
	case <-drained:
		t.Fatal("Drain skipped original package owner")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-returned; !errors.Is(err, context.Canceled) {
		t.Fatal("cancel result", err)
	}
	if err := <-drained; err != nil {
		t.Fatal("original package join", err)
	}
	noSkillTemporary(t, base)
	if _, err := os.Stat(filepath.Join(base, skillDirectory, skillRequest().SkillID, skillRequest().RevisionID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial version exposed")
	}
}
