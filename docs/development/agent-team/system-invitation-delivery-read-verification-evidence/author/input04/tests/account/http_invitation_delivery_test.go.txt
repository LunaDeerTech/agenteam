//go:build integration

package account_test

import (
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestAccountHTTPInvitationDeliveryDirectory(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.admin()
	member := f.invite(admin, "directory-member@example.test", "directory-member")
	const path = "/api/v1/system/invitations"
	empty := admin.request("GET", path, nil, "", "").want(t, 200).object(t)
	if len(empty) != 1 || len(httpItems(t, empty)) != 0 {
		t.Fatal("redeemed invitation visible or empty shape changed")
	}
	receipts := []map[string]any{}
	for _, email := range []string{"delivery-one@example.test", "delivery-two@example.test"} {
		receipt := admin.request("POST", path, map[string]any{"email": email}, id[struct{}](t).String(), admin.csrf).want(t, 201).object(t)
		if len(receipt) != 3 {
			t.Fatal("InvitationReceipt changed")
		}
		receipts = append(receipts, receipt)
		admin.waitMail(httpString(t, receipt, "job_id"), "sent")
	}
	conn := f.db.Connect(t)
	for _, raw := range httpItems(t, admin.request("GET", path, nil, "", "").want(t, 200).object(t)) {
		row := raw.(map[string]any)
		if len(row) != 6 {
			t.Fatal("Invitation not six fields")
		}
		delivery := httpObject(t, row, "latest_delivery")
		if len(delivery) != 8 || delivery["channel"] != "backend_log" || delivery["attempt_result"] != "sent" || delivery["phase"] != "sent" || delivery["attempts"] != "1" {
			t.Fatal("actual log attempt projection", delivery)
		}
		var email, job string
		var version int64
		var created, expires, accepted time.Time
		e := conn.QueryRow(ctxFor(t), `SELECT v.canonical_email,v.version,v.created_at,v.expires_at,i.job_id::text,c.completed_at FROM agenteam_account.invitations v JOIN agenteam_account.delivery_intents i ON i.link_id=v.id JOIN agenteam_account.commands c ON c.id=i.id WHERE v.id=$1`, row["id"]).Scan(&email, &version, &created, &expires, &job, &accepted)
		if e != nil {
			t.Fatal(e)
		}
		canon := func(at time.Time) string {
			v, e := foundation.NewInstant(at)
			if e != nil {
				t.Fatal(e)
			}
			return v.String()
		}
		if row["email"] != email || row["version"] != strconv.FormatInt(version, 10) || row["created_at"] != canon(created) || row["expires_at"] != canon(expires) || delivery["job_id"] != job || delivery["accepted_at"] != canon(accepted) {
			t.Fatal("wire differs from persisted safe facts")
		}
	}
	// Equal-time fixture changes neither versions nor immutable delivery facts.
	if _, e := conn.Exec(ctxFor(t), `WITH at AS MATERIALIZED (SELECT date_trunc('second',clock_timestamp()) AS value) UPDATE agenteam_account.invitations SET created_at=at.value-interval '1 hour',expires_at=at.value+interval '23 hours' FROM at`); e != nil {
		t.Fatal(e)
	}
	page := admin.request("GET", path+"?limit=1", nil, "", "").want(t, 200).object(t)
	cursor := httpString(t, page, "next_cursor")
	first := httpItems(t, page)[0].(map[string]any)
	firstID := httpString(t, first, "id")
	second := admin.request("GET", path+"?limit=100&cursor="+url.QueryEscape(cursor), nil, "", "").want(t, 200).object(t)
	if len(httpItems(t, second)) != 1 || httpItems(t, second)[0].(map[string]any)["id"] == firstID {
		t.Fatal("equal-time pagination")
	}
	if _, e := conn.Exec(ctxFor(t), `UPDATE agenteam_account.invitations SET last_delivery_at=clock_timestamp()-interval '61 seconds' WHERE id=$1`, firstID); e != nil {
		t.Fatal(e)
	}
	resend := admin.request("POST", path+"/"+firstID+"/resend", map[string]any{"version": first["version"]}, id[struct{}](t).String(), admin.csrf).want(t, 202).object(t)
	admin.waitMail(httpString(t, resend, "job_id"), "sent")
	changed := admin.request("GET", path+"?limit=1", nil, "", "").want(t, 200).object(t)
	if changed["next_cursor"] != cursor || httpObject(t, httpItems(t, changed)[0].(map[string]any), "latest_delivery")["job_id"] != resend["job_id"] {
		t.Fatal("delivery moved invitation boundary or selected old root")
	}
	for _, query := range []string{"limit=01", "limit=101", "cursor=bad", "cursor=" + url.QueryEscape(cursor+"x")} {
		status := admin.request("GET", path+"?"+query, nil, "", "")
		status.want(t, 400)
	}
	users := admin.request("GET", "/api/v1/system/users?limit=1", nil, "", "").want(t, 200).object(t)
	admin.request("GET", path+"?cursor="+url.QueryEscape(httpString(t, users, "next_cursor")), nil, "", "").problem(t, 400, "CURSOR_INVALID")
	member.request("GET", path, nil, "", "").problem(t, 403, "FORBIDDEN")
	f.browser().request("GET", path, nil, "", "").problem(t, 401, "UNAUTHENTICATED")
	head := admin.request("HEAD", path, nil, "", "").want(t, 405)
	if len(head.data) != 0 || head.headers.Get("Allow") != "GET, POST" {
		t.Fatal("invitation HEAD changed")
	}
	// Owned invalid accepted time cannot fall back to an older successful cycle.
	if _, e := conn.Exec(ctxFor(t), `UPDATE agenteam_account.commands SET completed_at='10000-01-01Z'::timestamptz WHERE attempt_id=$1`, resend["job_id"]); e != nil {
		t.Fatal(e)
	}
	bad := admin.request("GET", path, nil, "", "")
	bad.problem(t, 503, "DEPENDENCY_UNAVAILABLE")
	for _, name := range []string{"items", "next_cursor", "latest_delivery"} {
		if _, ok := bad.object(t)[name]; ok {
			t.Fatal("failure exposed candidate", name)
		}
	}
	if _, e := conn.Exec(ctxFor(t), `UPDATE agenteam_account.commands SET completed_at=clock_timestamp() WHERE attempt_id=$1`, resend["job_id"]); e != nil {
		t.Fatal(e)
	}
	admin.request("POST", path+"/"+firstID+"/revoke", map[string]any{"version": resend["version"]}, id[struct{}](t).String(), admin.csrf).want(t, 204)
	remaining := httpItems(t, admin.request("GET", path, nil, "", "").want(t, 200).object(t))
	if len(remaining) != 1 || remaining[0].(map[string]any)["id"] == firstID {
		t.Fatal("revoke visible")
	}
	// Expire without invoking cleanup: GET must not delete or modify history.
	if _, e := conn.Exec(ctxFor(t), `WITH at AS MATERIALIZED (SELECT clock_timestamp() AS value) UPDATE agenteam_account.invitations SET created_at=at.value-interval '25 hours',expires_at=at.value-interval '1 hour' FROM at WHERE id=$1`, remaining[0].(map[string]any)["id"]); e != nil {
		t.Fatal(e)
	}
	if len(httpItems(t, admin.request("GET", path, nil, "", "").want(t, 200).object(t))) != 0 {
		t.Fatal("expired row visible")
	}
	var retained int
	if e := conn.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.invitations`).Scan(&retained); e != nil || retained != 1 {
		t.Fatal("GET mutated expired row", retained, e)
	}
	if _, e := conn.Exec(ctxFor(t), `UPDATE agenteam_account.users SET role='user' WHERE email='admin@mail.com'`); e != nil {
		t.Fatal(e)
	}
	admin.request("GET", path+"?cursor="+url.QueryEscape(cursor), nil, "", "").problem(t, 403, "FORBIDDEN")
	if _, e := conn.Exec(ctxFor(t), `UPDATE agenteam_account.users SET role='admin' WHERE email='admin@mail.com'`); e != nil {
		t.Fatal(e)
	}
	revoked := admin.cookie("agenteam_local_session")
	admin.request("POST", "/api/v1/sessions/logout", map[string]any{}, id[struct{}](t).String(), admin.csrf).want(t, 204)
	admin.setCookie("agenteam_local_session", revoked)
	admin.request("GET", path, nil, "", "").problem(t, 401, "SESSION_REVOKED")
}
