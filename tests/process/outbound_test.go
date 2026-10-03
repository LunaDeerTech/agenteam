package process_test

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func TestCentralOutboundCARejectedBeforeDatabaseOrCheck(t *testing.T) {
	for _, check := range []bool{false, true} {
		listener, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		accepted := make(chan bool, 1)
		go func() {
			conn, e := listener.Accept()
			if e == nil {
				conn.Close()
			}
			accepted <- e == nil
		}()
		env := []string{"AGENTEAM_CENTRAL_DATABASE_URL=postgresql://unit:password-SENTINEL@" + listener.Addr().String() + "/unit", "AGENTEAM_CENTRAL_DATABASE_TLS_MODE=disable", `AGENTEAM_CENTRAL_CURSOR_KEYRING={"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`, `AGENTEAM_CENTRAL_SECRET_KEYRING={"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE=" + filepath.Join(t.TempDir(), "CA-SENTINEL.pem")}
		var args []string
		if check {
			args = []string{"--check-config"}
		}
		p := launch(t, "agenteam", args, env)
		p.wait(t, 2)
		listener.Close()
		if <-accepted {
			t.Fatal("invalid CA attempted DB connection")
		}
		logs := p.stdout.String() + p.stderr.String()
		if strings.Contains(logs, "SENTINEL") || !strings.Contains(logs, `"field":"AGENTEAM_CENTRAL_OUTBOUND_CA_FILE"`) {
			t.Fatal("unsafe/missing CA error")
		}
	}
}
