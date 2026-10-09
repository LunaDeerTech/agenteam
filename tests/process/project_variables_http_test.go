//go:build integration

package process_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// Uses the existing real no-tag cmd executable, Account login and owned seven-
// resource chain. Project is prepared by its real service with the disclosed
// persisted test Skills port; this does not claim production Skills delivery.
func TestProjectVariablesHTTPProcessRoutingAndPersistence(t *testing.T) {
	v := newModelSystemBinary(t)
	p := workRootProject(t, v)
	base := "/api/v1/projects/" + p.ID.String()
	path := base + "/variables"
	privateName, privateDescription, privateValue := "ROOT_VARIABLE_CANARY", "private-variable-description-canary", "private-variable-value-canary"
	v.logSecrets = append(v.logSecrets, privateName, privateDescription, privateValue)
	request := func(method, suffix, key string, input any) modelSystemResponse {
		t.Helper()
		r := v.request(t, method, path+suffix, key, input, nil).want(t, 200)
		if r.header.Get("Cache-Control") != "no-store" || r.header.Get("Content-Length") == "" || r.header.Get("X-Request-ID") == "" {
			t.Fatal("Variable root lost public output boundary")
		}
		if method == "HEAD" && len(r.body) != 0 {
			t.Fatal("HEAD emitted body")
		}
		return r
	}
	decode := func(r modelSystemResponse, out any) {
		t.Helper()
		if e := json.Unmarshal(r.body, out); e != nil {
			t.Fatal("invalid typed Variable response", e)
		}
	}
	id, key := modelSystemKey(t), modelSystemKey(t)
	v.logSecrets = append(v.logSecrets, key)
	original := map[string]any{"variable_id": id, "name": privateName, "description": privateDescription, "value": privateValue}
	createdResponse := request("POST", "", key, map[string]any{"request": original})
	var created vc.VariableMutation
	decode(createdResponse, &created)
	if created.Fields().Variable.Fields().ID.String() != id || created.Fields().Variable.Fields().Version != 1 || !created.Fields().Changed {
		t.Fatal("create result did not match original")
	}
	var detail vc.Variable
	decode(request("GET", "/"+id, "", nil), &detail)
	if detail.Fields().Value != privateValue {
		t.Fatal("authorized detail lost ordinary value")
	}
	var page f.Page[vc.VariableSummary]
	listed := request("GET", "?limit=1", "", nil)
	decode(listed, &page)
	if len(page.Items) != 1 || page.NextCursor != "" || page.Items[0].Fields().ID.String() != id || bytes.Contains(listed.body, []byte(privateValue)) {
		t.Fatal("summary page leaked value or lost item")
	}
	request("HEAD", "", "", nil)
	request("HEAD", "/"+id, "", nil)
	updateKey := modelSystemKey(t)
	patch := map[string]any{"description": "", "value": "root-variable-later-canary"}
	v.logSecrets = append(v.logSecrets, updateKey, "root-variable-later-canary")
	updateBody := map[string]any{"expected_version": "1", "request": patch}
	updatedResponse := request("PATCH", "/"+id, updateKey, updateBody)
	var updated vc.VariableMutation
	decode(updatedResponse, &updated)
	if updated.Fields().Variable.Fields().Version != 2 || updated.Fields().Variable.Fields().Description != "" {
		t.Fatal("update lost presence or version")
	}
	noop := request("PATCH", "/"+id, modelSystemKey(t), map[string]any{"expected_version": "2", "request": patch})
	var no vc.VariableMutation
	decode(noop, &no)
	if no.Fields().Changed || no.Fields().EventID != nil || no.Fields().AuditID != nil || no.Fields().Variable.Fields().Version != 2 {
		t.Fatal("no-op created a second fact")
	}
	deleteKey := modelSystemKey(t)
	deleteBody := map[string]any{"expected_version": "2"}
	deletedResponse := request("DELETE", "/"+id, deleteKey, deleteBody)
	var deleted vc.VariableMutation
	decode(deletedResponse, &deleted)
	if deleted.Fields().Deleted == nil || deleted.Fields().Deleted.Version != 3 {
		t.Fatal("delete lost tombstone version")
	}
	v.request(t, "GET", path+"/"+id, "", nil, nil).want(t, 404)
	for _, originalCommand := range []struct {
		command, key, method string
		lookup, body         map[string]any
		receipt              modelSystemResponse
	}{
		{string(vc.CreateCommand), key, "POST", map[string]any{"command": string(vc.CreateCommand), "request": original}, map[string]any{"request": original}, createdResponse},
		{string(vc.UpdateCommand), updateKey, "PATCH", map[string]any{"command": string(vc.UpdateCommand), "target_id": id, "expected_version": "1", "request": patch}, updateBody, updatedResponse},
		{string(vc.DeleteCommand), deleteKey, "DELETE", map[string]any{"command": string(vc.DeleteCommand), "target_id": id, "expected_version": "2"}, deleteBody, deletedResponse},
	} {
		var got vc.VariableCommandLookup
		decode(request("POST", "/commands/lookup", originalCommand.key, originalCommand.lookup), &got)
		if got.Status() != vc.LookupCommitted || got.Receipt() == nil {
			t.Fatal("root original lookup did not commit", originalCommand.command)
		}
		saved, e := json.Marshal(got.Receipt())
		if e != nil || !bytes.Equal(saved, originalCommand.receipt.body) {
			t.Fatal("historical receipt was replaced by current target", e)
		}
		suffix := "/" + id
		if originalCommand.method == "POST" {
			suffix = ""
		}
		if replay := request(originalCommand.method, suffix, originalCommand.key, originalCommand.body); !bytes.Equal(replay.body, originalCommand.receipt.body) {
			t.Fatal("explicit original replay changed receipt")
		}
	}
	auditResponse := v.request(t, "GET", base+"/audit?limit=100", "", nil, nil).want(t, 200)
	var auditPage struct {
		Items []struct {
			Action   string          `json:"action"`
			Metadata json.RawMessage `json:"metadata"`
		} `json:"items"`
	}
	if e := json.Unmarshal(auditResponse.body, &auditPage); e != nil {
		t.Fatal(e)
	}
	actions := map[string]int{}
	for _, item := range auditPage.Items {
		if strings.HasPrefix(item.Action, "project.variable.") {
			actions[item.Action]++
		}
	}
	if actions[string(vc.CreateCommand)] != 1 || actions[string(vc.UpdateCommand)] != 1 || actions[string(vc.DeleteCommand)] != 1 || len(actions) != 3 {
		t.Fatal("root Project Audit did not consume three safe Variable actions")
	}
	for _, private := range []string{privateName, privateDescription, privateValue, "root-variable-later-canary", key, updateKey, deleteKey} {
		if bytes.Contains(auditResponse.body, []byte(private)) {
			t.Fatal("Audit response exposed Variable input")
		}
	}
	var commands, history, audits, events int
	if e := v.db.Connect(t).QueryRow(databaseContext(t), `SELECT (SELECT count(*) FROM agenteam_projectvariable.commands WHERE project_id=$1 AND state='completed'),(SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND producer='projectvariable'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND producer='projectvariable')`, p.ID.String()).Scan(&commands, &history, &audits, &events); e != nil || commands != 4 || history != 3 || audits != 3 || events != 3 {
		t.Fatal("root durable facts differ", commands, history, audits, events, e)
	}
	// A representative existing route in each adjacent default assembly remains available.
	v.request(t, "GET", "/api/v1/session", "", nil, nil).want(t, 200)
	v.request(t, "GET", base+"/milestones", "", nil, nil).want(t, 200)
	v.request(t, "GET", base+"/model-providers", "", nil, nil).want(t, 200)
	t.Run("three-real-tcp-responses-abandoned-before-eof", func(t *testing.T) {
		lostID := modelSystemKey(t)
		create := map[string]any{"variable_id": lostID, "name": "ROOT_LOSS_CANARY", "description": "", "value": "root-loss-value-canary"}
		patch := map[string]any{"value": "root-loss-updated-canary"}
		v.logSecrets = append(v.logSecrets, "ROOT_LOSS_CANARY", "root-loss-value-canary", "root-loss-updated-canary")
		for _, in := range []struct {
			method, command, suffix string
			body, lookup            map[string]any
		}{
			{"POST", string(vc.CreateCommand), "", map[string]any{"request": create}, map[string]any{"command": string(vc.CreateCommand), "request": create}},
			{"PATCH", string(vc.UpdateCommand), "/" + lostID, map[string]any{"expected_version": "1", "request": patch}, map[string]any{"command": string(vc.UpdateCommand), "target_id": lostID, "expected_version": "1", "request": patch}},
			{"DELETE", string(vc.DeleteCommand), "/" + lostID, map[string]any{"expected_version": "2"}, map[string]any{"command": string(vc.DeleteCommand), "target_id": lostID, "expected_version": "2"}},
		} {
			key := modelSystemKey(t)
			v.logSecrets = append(v.logSecrets, key)
			variableRootAbandonResponse(t, v, in.method, path+in.suffix, key, in.body)
			// Real login creates a new Session; the saved request/key/version is unchanged.
			v.login(t)
			var recovered vc.VariableCommandLookup
			decode(request("POST", "/commands/lookup", key, in.lookup), &recovered)
			if recovered.Status() != vc.LookupCommitted || recovered.Receipt() == nil || string(recovered.Receipt().Fields().Command) != in.command {
				t.Fatal("lost actual response did not recover original receipt")
			}
			saved, e := json.Marshal(recovered.Receipt())
			if e != nil {
				t.Fatal(e)
			}
			if replay := request(in.method, in.suffix, key, in.body); !bytes.Equal(saved, replay.body) {
				t.Fatal("lost response original replay changed receipt")
			}
			var command, history, event, audit int
			if e := v.db.Connect(t).QueryRow(databaseContext(t), `SELECT count(*),coalesce(sum((SELECT count(*) FROM agenteam_projectvariable.history h WHERE h.operation_id=c.id)),0),coalesce(sum((SELECT count(*) FROM agenteam_outbox.events e WHERE e.id=c.event_id)),0),coalesce(sum((SELECT count(*) FROM agenteam_audit.audit_records a WHERE a.id=(c.receipt->>'audit_id')::uuid)),0) FROM agenteam_projectvariable.commands c WHERE c.project_id=$1 AND c.command_name=$2 AND c.idempotency_key=$3 AND c.state='completed'`, p.ID.String(), in.command, key).Scan(&command, &history, &event, &audit); e != nil || command != 1 || history != 1 || event != 1 || audit != 1 {
				t.Fatal("lost response duplicated or lost durable facts", command, history, event, audit, e)
			}
		}
	})
	v.stop(t, syscall.SIGTERM)
}

// The client observes valid success headers and one body byte, then closes the
// actual TCP connection without observing the declared Content-Length or EOF.
// No typed receipt or success body is injected; recovery starts from saved intent.
func variableRootAbandonResponse(t *testing.T, v *modelSystemBinary, method, path, key string, input any) {
	t.Helper()
	destination, e := url.Parse(v.address)
	if e != nil {
		t.Fatal(e)
	}
	conn, e := net.DialTimeout("tcp", destination.Host, 5*time.Second)
	if e != nil {
		t.Fatal("actual root TCP connect", e)
	}
	defer conn.Close()
	if e = conn.SetDeadline(time.Now().Add(5 * time.Second)); e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(input)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(raw)
	req, e := http.NewRequest(method, v.address+path, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:8080")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", v.csrf)
	req.Header.Set("Idempotency-Key", key)
	for _, cookie := range v.cookies {
		req.AddCookie(cookie)
	}
	if e = req.Write(conn); e != nil {
		t.Fatal("actual request write", e)
	}
	response, e := http.ReadResponse(bufio.NewReader(conn), req)
	if e != nil {
		t.Fatal("actual response headers", e)
	}
	if response.StatusCode != 200 || response.ContentLength <= 1 {
		conn.Close()
		response.Body.Close()
		t.Fatal("loss stimulus did not reach complete committed response headers", response.StatusCode)
	}
	var first [1]byte
	if _, e = io.ReadFull(response.Body, first[:]); e != nil {
		conn.Close()
		response.Body.Close()
		t.Fatal("actual response first byte", e)
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		if e = tcp.SetLinger(0); e != nil {
			t.Fatal(e)
		}
	}
	if e = conn.Close(); e != nil {
		t.Fatal("actual client close", e)
	}
	_ = response.Body.Close()
}
