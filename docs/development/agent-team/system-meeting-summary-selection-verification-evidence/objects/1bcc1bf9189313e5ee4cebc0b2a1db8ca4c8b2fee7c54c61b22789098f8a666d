//go:build integration

package model_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type meetingSummaryOldSelection struct {
	Scope, Key      string
	ExpectedVersion f.Version
	Selection       mc.PlatformSelection
}
type meetingSummaryOldDelete struct {
	Scope, Key, ID  string
	ExpectedVersion f.Version
	Replacement     *string
}
type meetingSummaryOldHandoff struct {
	User, Session    string
	Selection        meetingSummaryOldSelection
	SelectionReceipt mc.CommandReceipt
	Provider         meetingSummaryOldDelete
	ProviderReceipt  mc.CommandReceipt
	Model            meetingSummaryOldDelete
	ModelReceipt     mc.CommandReceipt
	History          map[string][]byte
}

func TestModelMeetingSummaryUpgradeReceipts(t *testing.T) {
	binary, want := os.Getenv("AGENTEAM_MEETING_SUMMARY_OLD_BINARY"), os.Getenv("AGENTEAM_MEETING_SUMMARY_OLD_SHA256")
	if !filepath.IsAbs(binary) || len(want) != 64 {
		t.Fatal("frozen fixed6fa old producer binary/hash required")
	}
	data, e := os.ReadFile(binary)
	if e != nil {
		t.Fatal("old binary missing")
	}
	sum := sha256.Sum256(data)
	clear(data)
	if hex.EncodeToString(sum[:]) != want {
		t.Fatal("old binary differs from frozen6fa producer")
	}
	// pgfixture.NewDatabase only creates an empty nonce-owned database. The old
	// binary alone migrates 1..19 and accepts the historical production commands.
	db := pgfixture.NewDatabase(t)
	dir := t.TempDir()
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	handoffPath := filepath.Join(dir, "handoff.json")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestMeetingSummaryOldProducer$", "-test.v", "-test.timeout=85s")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "AGENTEAM_MEETING_SUMMARY_OLD_DATABASE="+db.Name, "AGENTEAM_MEETING_SUMMARY_OLD_HANDOFF="+handoffPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if e = cmd.Start(); e != nil {
		t.Fatal("old producer start failed")
	}
	pid := cmd.Process.Pid
	e = cmd.Wait()
	exit := cmd.ProcessState.ExitCode()
	t.Logf("fixed6fa old producer pid=%d actual exit=%d stdout_sha256=%x stderr_sha256=%x", pid, exit, sha256.Sum256(stdout.Bytes()), sha256.Sum256(stderr.Bytes()))
	if e != nil {
		t.Fatalf("old producer failed, safe output: %s %s", stdout.String(), stderr.String())
	}
	stat, e := os.Stat(handoffPath)
	if e != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("private handoff not 0600")
	}
	raw, e := os.ReadFile(handoffPath)
	if e != nil {
		t.Fatal(e)
	}
	var handoff meetingSummaryOldHandoff
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	e = decoder.Decode(&handoff)
	var trailing any
	if e == nil && decoder.Decode(&trailing) != io.EOF {
		e = io.ErrUnexpectedEOF
	}
	clear(raw)
	if e != nil {
		t.Fatal("old handoff invalid")
	}
	if e = os.Remove(handoffPath); e != nil {
		t.Fatal("private handoff cleanup failed")
	}
	// No migration20 or candidate service has run before the old writer exits.
	conn := db.Connect(t)
	var max, count int64
	var absent bool
	if e = conn.QueryRow(testContext(t), `SELECT max(version),count(*),to_regclass('agenteam_model.meeting_summary_selection') IS NULL FROM agenteam_meta.migration_journal WHERE state='applied'`).Scan(&max, &count, &absent); e != nil || max != 19 || count != 19 || !absent {
		t.Fatal("not a real pre20 upgrade", e)
	}
	if len(handoff.History) != 3 {
		t.Fatal("incomplete old history tables")
	}
	history := func() {
		t.Helper()
		for table, previous := range handoff.History {
			if table != "agenteam_model.commands" && table != "agenteam_audit.audit_records" && table != "agenteam_outbox.events" {
				t.Fatal("unexpected old history table")
			}
			var rows map[string]json.RawMessage
			if json.Unmarshal(previous, &rows) != nil || len(rows) == 0 {
				t.Fatal("old history invalid")
			}
			ids := make([]string, 0, len(rows))
			for key := range rows {
				if _, e := f.ParseID[struct{}](key); e != nil {
					t.Fatal("old history ID invalid")
				}
				ids = append(ids, key)
			}
			var current []byte
			if e := conn.QueryRow(testContext(t), `SELECT coalesce(jsonb_object_agg(id::text,to_jsonb(r)),'{}'::jsonb) FROM `+table+` r WHERE id::text=ANY($1)`, ids).Scan(&current); e != nil || !bytes.Equal(current, previous) {
				t.Fatal("old plan/semantic/receipt/Audit/Event rewritten", table, e)
			}
			t.Logf("historical table=%s original_rows=%d original_sql_bytes_sha256=%x unchanged", table, len(ids), sha256.Sum256(previous))
		}
	}
	history()
	migrateModel(t, modelMigrator(t, db, modelMigrationFiles(t, "00020")), 20)
	history()
	store := openStore(t, db.Config(t, nil))
	v := assemble(t, db, store, store)
	user, e := f.ParseID[id.User](handoff.User)
	if e != nil {
		t.Fatal("old user invalid")
	}
	session, e := f.ParseID[id.Session](handoff.Session)
	if e != nil {
		t.Fatal("old session invalid")
	}
	actor, e := id.NewHuman(user, session)
	if e != nil {
		t.Fatal(e)
	}
	// Reconstructing a Human only supplies identity claims. Each production call
	// still rechecks the actual current Session and System administrator in PG.
	v.admin = actor
	if handoff.Selection.Scope != "system" || handoff.Provider.Scope != "system" || handoff.Model.Scope != "system" || handoff.Provider.Replacement != nil || handoff.Model.Replacement != nil {
		t.Fatal("unexpected old request scope/shape")
	}
	providerID, e := f.ParseID[mc.Provider](handoff.Provider.ID)
	if e != nil {
		t.Fatal("old provider ID invalid")
	}
	modelID, e := f.ParseID[mc.Model](handoff.Model.ID)
	if e != nil {
		t.Fatal("old model ID invalid")
	}
	meta := func(key string) mc.CommandMeta {
		return mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: f.IdempotencyKey(key)}
	}
	selectionRequest := mc.UpdatePlatformSelectionRequest{CommandMeta: meta(handoff.Selection.Key), ExpectedVersion: handoff.Selection.ExpectedVersion, Selection: handoff.Selection.Selection}
	providerRequest := mc.DeleteProviderRequest{CommandMeta: meta(handoff.Provider.Key), ID: providerID, ExpectedVersion: handoff.Provider.ExpectedVersion}
	modelRequest := mc.DeleteModelRequest{CommandMeta: meta(handoff.Model.Key), ID: modelID, ExpectedVersion: handoff.Model.ExpectedVersion}
	if selectionRequest.Validate() != nil || providerRequest.Validate() != nil || modelRequest.Validate() != nil {
		t.Fatal("old request data invalid")
	}
	for _, pair := range []struct {
		receipt        mc.CommandReceipt
		kind, resource string
		version        f.Version
	}{{handoff.SelectionReceipt, "model.selection.update", selectionRequest.Selection.ID, selectionRequest.ExpectedVersion + 1}, {handoff.ProviderReceipt, "provider.delete", providerID.String(), providerRequest.ExpectedVersion + 1}, {handoff.ModelReceipt, "model.delete", modelID.String(), modelRequest.ExpectedVersion + 1}} {
		if pair.receipt.Validate() != nil || pair.receipt.Kind != pair.kind || pair.receipt.ResourceID != pair.resource || pair.receipt.Version != pair.version || pair.receipt.AffectedReferences != 0 {
			t.Fatal("old receipt data invalid")
		}
	}
	replay := func() {
		a, e := v.service.UpdatePlatformSelection(testContext(t), selectionRequest)
		if e != nil || a != handoff.SelectionReceipt {
			t.Fatal("old selection replay", e)
		}
		b, e := v.service.DeleteProvider(testContext(t), providerRequest)
		if e != nil || b != handoff.ProviderReceipt {
			t.Fatal("old provider-delete replay", e)
		}
		c, e := v.service.DeleteModel(testContext(t), modelRequest)
		if e != nil || c != handoff.ModelReceipt {
			t.Fatal("old model-delete replay", e)
		}
		lookup, e := v.service.LookupCommand(testContext(t), model.LookupCommandRequest{Meta: selectionRequest.CommandMeta, Command: "model.selection.update"})
		if e != nil || !lookup.Found || lookup.Receipt == nil || *lookup.Receipt != handoff.SelectionReceipt {
			t.Fatal("old lookup", e)
		}
	}
	replay()
	if e = v.service.InitializeMeetingSummarySelection(testContext(t)); e != nil {
		t.Fatal(e)
	}
	summary, e := v.service.GetMeetingSummarySelection(testContext(t), actor)
	if e != nil {
		t.Fatal(e)
	}
	request := mc.UpdateMeetingSummarySelectionRequest{CommandMeta: selectionRequest.CommandMeta, SelectionID: summary.ID, ExpectedVersion: summary.Version, Model: handoff.Selection.Selection.Memory}
	_, e = v.service.UpdateMeetingSummarySelection(testContext(t), request)
	requireCode(t, e, f.IdempotencyKeyReused)
	request.Key = "new-summary-choice"
	if _, e = v.service.UpdateMeetingSummarySelection(testContext(t), request); e != nil {
		t.Fatal(e)
	}
	provider := v.provider(t, mc.OpenAIChat, nil)
	replacement := v.model(t, provider, mc.ChatModel)
	_, e = v.service.DeleteModel(testContext(t), mc.DeleteModelRequest{CommandMeta: v.meta(t, "upgrade-both-owners"), ID: handoff.Selection.Selection.Memory, ExpectedVersion: 1, Replacement: &replacement.ID})
	if e != nil {
		t.Fatal("cross owner upgrade replacement", e)
	}
	replay()
	history()
}
