package account

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func material(t *testing.T, s string) sc.SecretMaterial {
	t.Helper()
	m, e := sc.NewSecretMaterial([]byte(s))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Destroy)
	return m
}
func codeIs(err error, code foundation.Code) bool {
	var f *foundation.Fault
	return errors.As(err, &f) && f != nil && f.Code == code
}
func TestPasswordRules(t *testing.T) {
	for _, s := range []string{"a correct horse-shaped battery", "  No automatic trimming  ", "自由的密码可以包含中文并保持原样不规范化", strings.Repeat("界", 127) + "x"} {
		if e := ValidatePassword([]byte(s)); e != nil {
			t.Fatalf("valid length %d rejected: %v", len(s), e)
		}
	}
	for _, s := range []string{"short", strings.Repeat("x", 129), strings.Repeat("😀", 129), "aaaaaaaaaaaaaaaa", "abcdabcdabcdabcd", "0123456789012345", "zyxwvutsrqponmlk", "123456789987654321", "invalid utf8 long\xff"} {
		if e := ValidatePassword([]byte(s)); e == nil {
			t.Fatalf("weak/invalid password accepted length=%d", len(s))
		}
	}
}
func TestPasswordHashActualParametersAndVerification(t *testing.T) {
	h := NewPasswordHasher()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	good := material(t, "a correct horse-shaped battery")
	a, e := h.Hash(ctx, good)
	if e != nil {
		t.Fatal(e)
	}
	b, e := h.Hash(ctx, good)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(a.encoded(), phcPrefix) || a.encoded() == b.encoded() {
		t.Fatal("salt/parameters")
	}
	salt, digest, e := parsePHC(a.encoded())
	if e != nil || len(salt) != 16 || len(digest) != 32 {
		t.Fatal("PHC")
	}
	clear(salt)
	clear(digest)
	if ok, e := h.Verify(ctx, good, a.encoded()); e != nil || !ok {
		t.Fatalf("verify %v %v", ok, e)
	}
	if ok, e := h.Verify(ctx, material(t, "another correct password"), a.encoded()); e != nil || ok {
		t.Fatalf("wrong verify %v %v", ok, e)
	}
	if ok, e := h.Verify(ctx, good, dummyPHC); e != nil || ok {
		t.Fatalf("dummy %v %v", ok, e)
	}
	for _, bad := range []string{strings.Replace(a.encoded(), "m=65536", "m=4294967295", 1), strings.Replace(a.encoded(), "p=4", "p=0", 1), a.encoded() + "=", phcPrefix + base64.RawStdEncoding.EncodeToString(make([]byte, 15)) + "$" + base64.RawStdEncoding.EncodeToString(make([]byte, 32))} {
		if _, e := h.Verify(ctx, good, bad); !codeIs(e, foundation.DependencyUnavailable) {
			t.Fatalf("bad PHC: %v", e)
		}
	}
	for _, v := range []any{a, &a, struct{ p PasswordHash }{a}} {
		if strings.Contains(fmt.Sprintf("%#v", v), "argon2") {
			t.Fatal("PHC leaked")
		}
	}
}
func TestHasherGlobalCapacityAndActualJoin(t *testing.T) {
	h1, h2 := NewPasswordHasher(), NewPasswordHasher()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan error, 2)
	for _, h := range []*PasswordHasher{h1, h2} {
		go func(h *PasswordHasher) {
			done <- h.withSlot(context.Background(), func(ctx context.Context) error { entered <- struct{}{}; <-release; return ctx.Err() })
		}(h)
	}
	<-entered
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if e := h1.Force(ctx); e == nil || h1.Joined() {
		t.Fatal("force pretended a running hash joined")
	}
	if e := h1.withSlot(context.Background(), func(context.Context) error { return nil }); !codeIs(e, foundation.ShuttingDown) {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	queueCtx, stop := context.WithCancel(context.Background())
	for range 16 {
		wg.Go(func() {
			_ = h2.withSlot(queueCtx, func(context.Context) error { t.Error("queued work ran before slot was released"); return nil })
		})
	}
	deadline := time.Now().Add(time.Second)
	for len(hashCapacity) != 18 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(hashCapacity) != 18 {
		t.Fatal("queue did not fill")
	}
	if e := h2.withSlot(context.Background(), func(context.Context) error { t.Error("overflow ran"); return nil }); !codeIs(e, foundation.RateLimited) {
		t.Fatal(e)
	}
	stop()
	wg.Wait()
	close(release)
	<-done
	<-done
	if !h1.Joined() || !h2.Joined() || len(hashCapacity) != 0 || len(hashSlots) != 0 {
		t.Fatal("hash ownership leaked")
	}
}
