//go:build integration

package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestAccountCaptchaOfficialVueActualBrowser(t *testing.T) {
	// Both cases share the original overall budget, but neither shares an
	// account, failure counter, challenge, HTTP server, or database with the other.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, name := range []string{"desktop", "keyboard"} {
		t.Run(name, func(t *testing.T) { runCaptchaBrowserCase(t, ctx, name) })
	}
}

func runCaptchaBrowserCase(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	if err := ctx.Err(); err != nil {
		t.Fatal("shared browser budget", err)
	}
	f := newB02Account(t)
	password := f.bootstrap(t)
	if _, e := f.store.Exec(ctxFor(t), `UPDATE agenteam_account.account_settings SET challenge_after_failures=1`); e != nil {
		t.Fatal(e)
	}
	root, e := filepath.Abs("../account-captcha-web")
	if e != nil {
		t.Fatal(e)
	}
	var logins, verified atomic.Int32
	mux := http.NewServeMux()
	var origin string
	write := func(w http.ResponseWriter, value any, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			code := foundation.InternalError
			var fault *foundation.Fault
			if errors.As(err, &fault) && fault != nil {
				code = fault.Code
			}
			w.WriteHeader(400)
			value = map[string]string{"code": string(code)}
		}
		_ = json.NewEncoder(w).Encode(value)
	}
	token := func(raw string) (sc.SecretMaterial, error) { return sc.NewSecretMaterial([]byte(raw)) }
	browser := func(r *http.Request) (c.BrowserIdentity, error) {
		if r.Method != "POST" || r.Header.Get("Origin") != origin {
			return c.BrowserIdentity{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
		}
		cookie, e := r.Cookie("fixture_anonymous")
		if e != nil {
			return c.BrowserIdentity{}, e
		}
		m, e := token(cookie.Value)
		if e != nil {
			return c.BrowserIdentity{}, e
		}
		defer m.Destroy()
		csrf, e := token(r.Header.Get("X-CSRF-Token"))
		if e != nil {
			return c.BrowserIdentity{}, e
		}
		defer csrf.Destroy()
		return f.service.VerifyAnonymousContext(r.Context(), m, csrf)
	}
	decode := func(r *http.Request, v any) error {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 16385))
		if e != nil || len(raw) > 16384 {
			return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
		}
		defer clear(raw)
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.DisallowUnknownFields()
		if e = d.Decode(v); e != nil {
			return e
		}
		if d.Decode(new(any)) != io.EOF {
			return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
		}
		return nil
	}
	mux.HandleFunc("GET /fixture/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		a, e := f.service.NewAnonymousContext(r.Context())
		if e != nil {
			write(w, nil, e)
			return
		}
		defer a.Cookie.Destroy()
		defer a.CSRF.Destroy()
		var csrf string
		e = a.Cookie.Use(func(b []byte) error {
			http.SetCookie(w, &http.Cookie{Name: "fixture_anonymous", Value: string(b), HttpOnly: true, SameSite: http.SameSiteLaxMode, Path: "/"})
			return nil
		})
		if e == nil {
			e = a.CSRF.Use(func(b []byte) error { csrf = string(b); return nil })
		}
		write(w, map[string]string{"csrf": csrf}, e)
	})
	mux.HandleFunc("POST /fixture/challenge", func(w http.ResponseWriter, r *http.Request) {
		b, e := browser(r)
		if e != nil {
			write(w, nil, e)
			return
		}
		var body struct {
			Email string `json:"email"`
			Key   string `json:"login_key"`
		}
		if e = decode(r, &body); e != nil {
			write(w, nil, e)
			return
		}
		q, e := c.NewChallengeRequest(c.ChallengeFields{Browser: b, Email: body.Email, LoginKey: foundation.IdempotencyKey(body.Key)})
		if e != nil {
			write(w, nil, e)
			return
		}
		v, e := f.service.CreateChallenge(r.Context(), q)
		write(w, v, e)
	})
	mux.HandleFunc("POST /fixture/verify", func(w http.ResponseWriter, r *http.Request) {
		b, e := browser(r)
		if e != nil {
			write(w, nil, e)
			return
		}
		var body struct {
			ID    string `json:"challenge_id"`
			Angle int    `json:"angle"`
			Email string `json:"email"`
			Key   string `json:"login_key"`
		}
		if e = decode(r, &body); e != nil {
			write(w, nil, e)
			return
		}
		id, e := foundation.ParseID[c.ChallengeRecord](body.ID)
		if e != nil {
			write(w, nil, e)
			return
		}
		q, e := c.NewChallengeRequest(c.ChallengeFields{Browser: b, Email: body.Email, LoginKey: foundation.IdempotencyKey(body.Key)})
		if e != nil {
			write(w, nil, e)
			return
		}
		p, e := f.service.VerifyChallenge(r.Context(), q, id, body.Angle)
		if e != nil {
			write(w, nil, e)
			return
		}
		defer p.Destroy()
		var value string
		e = p.Use(func(b []byte) error { value = string(b); return nil })
		if e == nil {
			verified.Add(1)
		}
		write(w, map[string]string{"pass": value}, e)
	})
	mux.HandleFunc("POST /fixture/login", func(w http.ResponseWriter, r *http.Request) {
		b, e := browser(r)
		if e != nil {
			write(w, nil, e)
			return
		}
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Key      string `json:"login_key"`
			Pass     string `json:"challenge_pass"`
		}
		if e = decode(r, &body); e != nil {
			write(w, nil, e)
			return
		}
		pw, e := token(body.Password)
		body.Password = ""
		if e != nil {
			write(w, nil, e)
			return
		}
		defer pw.Destroy()
		var pass sc.SecretMaterial
		if body.Pass != "" {
			pass, e = token(body.Pass)
			body.Pass = ""
			if e != nil {
				write(w, nil, e)
				return
			}
			defer pass.Destroy()
		}
		q, e := c.NewLoginRequest(c.LoginFields{Browser: b, Key: foundation.IdempotencyKey(body.Key), Email: body.Email, Password: pw, ChallengePass: pass, ClientIP: netip.MustParseAddr("192.0.2.25")})
		if e != nil {
			write(w, nil, e)
			return
		}
		response, e := f.service.Login(r.Context(), q)
		if e != nil {
			write(w, nil, e)
			return
		}
		defer response.Close(r.Context())
		e = response.UseCookie(func(b []byte) error {
			http.SetCookie(w, &http.Cookie{Name: "fixture_session", Value: string(b), HttpOnly: true, Path: "/", SameSite: http.SameSiteLaxMode})
			return nil
		})
		if e == nil {
			logins.Add(1)
		}
		write(w, map[string]bool{"logged_in": e == nil}, e)
	})
	mux.Handle("/", http.FileServer(http.Dir(filepath.Join(root, "dist"))))
	server := httptest.NewUnstartedServer(mux)
	origin = "http://" + server.Listener.Addr().String()
	server.Start()
	defer server.Close()
	shortTmp, e := os.MkdirTemp("/tmp", "acct-web-")
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := os.RemoveAll(shortTmp); e != nil {
			t.Error(e)
		}
		if _, e := os.Stat(shortTmp); !errors.Is(e, os.ErrNotExist) {
			t.Error("browser temp cleanup incomplete")
		}
	}()
	cmd := exec.CommandContext(ctx, "npm", "test")
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TMPDIR=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "ACCOUNT_CAPTCHA_BASE_URL="+server.URL, "ACCOUNT_CAPTCHA_CASE="+name, "TMPDIR="+shortTmp)
	e = password.Use(func(b []byte) error { cmd.Env = append(cmd.Env, "ACCOUNT_CAPTCHA_PASSWORD="+string(b)); return nil })
	if e != nil {
		t.Fatal(e)
	}
	output, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("actual browser: %v\n%s", e, output)
	}
	t.Log(string(output))
	cmd.Env = nil
	if logins.Load() != 1 || verified.Load() != 1 {
		t.Fatal("browser did not cross actual login/verify", logins.Load(), verified.Load())
	}
	var consumed int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.challenges WHERE phase='consumed'`).Scan(&consumed); e != nil || consumed != 1 {
		t.Fatal("missing atomic consumption", consumed, e)
	}
}
