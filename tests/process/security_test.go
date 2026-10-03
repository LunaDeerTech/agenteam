package process_test

import (
	"net"
	"strings"
	"testing"
)

func TestCentralCursorConfigurationRequiredBeforeAnyConnection(t *testing.T) {
	for _, raw := range []string{"", `{"format":1,"current_kid":"key-SENTINEL","keys":[]}`, `{"format":1,"format":1,"current_kid":"key-SENTINEL","keys":[]}`} {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		accepted := make(chan bool, 1)
		go func() {
			conn, err := listener.Accept()
			if err == nil {
				_ = conn.Close()
			}
			accepted <- err == nil
		}()
		env := []string{"AGENTEAM_CENTRAL_DATABASE_URL=postgresql://pure:password-SENTINEL@" + listener.Addr().String() + "/pure", "AGENTEAM_CENTRAL_DATABASE_TLS_MODE=disable"}
		if raw != "" {
			env = append(env, "AGENTEAM_CENTRAL_CURSOR_KEYRING="+raw)
		}
		p := launch(t, "agenteam", nil, env)
		p.wait(t, 2)
		_ = listener.Close()
		if <-accepted {
			t.Fatal("invalid keyring reached database")
		}
		logs := p.stderr.String() + p.stdout.String()
		if strings.Contains(logs, "SENTINEL") || strings.Contains(logs, `"event":"listening"`) || !strings.Contains(logs, `"field":"AGENTEAM_CENTRAL_CURSOR_KEYRING"`) {
			t.Fatal("configuration diagnostic unsafe or incorrect")
		}
	}
}
