package object

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

func downloadTestMaterial(b byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32))
}
func downloadTestInputs(t *testing.T) (cursor.Keyring, secret.Keyring) {
	t.Helper()
	c, err := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"p2","keys":[{"kid":"p1","key_b64":%q},{"kid":"p2","key_b64":%q}]}`, downloadTestMaterial(1), downloadTestMaterial(2)))
	if err != nil {
		t.Fatal(err)
	}
	s, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"2","keys":[{"version":"1","key_b64":%q},{"version":"2","key_b64":%q}]}`, downloadTestMaterial(3), downloadTestMaterial(4)), c)
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}
func downloadTestKeys(t *testing.T) DownloadKeyring {
	t.Helper()
	c, s := downloadTestInputs(t)
	k, err := LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"new","keys":[{"kid":"old","key_b64":%q},{"kid":"new","key_b64":%q}]}`, downloadTestMaterial(5), downloadTestMaterial(6)), c, s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestDownloadKeyringBoundaries(t *testing.T) {
	c, s := downloadTestInputs(t)
	raw := fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, downloadTestMaterial(9))
	if _, err := LoadDownloadKeyring(raw, c, s); err != nil {
		t.Fatal(err)
	}
	bad := []string{"", "null", `[]`, raw + `{}`, strings.Replace(raw, `"format":1`, `"format":1,"format":1`, 1), strings.Replace(raw, `"format":1`, `"format":null`, 1), strings.Replace(raw, `"format":1`, `"format":1,"extra":true`, 1), strings.Replace(raw, `"current_kid":"a"`, `"current_kid":"missing"`, 1), strings.Replace(raw, `"current_kid":"a"`, `"current_kid":"a="`, 1), strings.Replace(raw, `"keys":[`, `"Keys":[`, 1), strings.Replace(raw, `"kid":"a"`, `"kid":null`, 1), strings.Replace(raw, downloadTestMaterial(9), strings.TrimSuffix(downloadTestMaterial(9), "="), 1), strings.Repeat(" ", 16<<10) + raw}
	for _, b := range []byte{1, 2, 3, 4} {
		bad = append(bad, strings.Replace(raw, downloadTestMaterial(9), downloadTestMaterial(b), 1))
	}
	for _, kid := range []string{"a", "b"} {
		bad = append(bad, fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q},{"kid":%q,"key_b64":%q}]}`, downloadTestMaterial(9), kid, downloadTestMaterial(9)))
	}
	var items []string
	for i := 0; i < 33; i++ {
		items = append(items, fmt.Sprintf(`{"kid":"k%d","key_b64":%q}`, i, downloadTestMaterial(byte(10+i))))
	}
	bad = append(bad, `{"format":1,"current_kid":"k0","keys":[`+strings.Join(items, ",")+`]}`)
	for i, input := range bad {
		if _, err := LoadDownloadKeyring(input, c, s); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
	if _, err := LoadDownloadKeyring(raw, cursor.Keyring{}, s); err == nil {
		t.Fatal("missing cursor keyring accepted")
	}
	if _, err := LoadDownloadKeyring(raw, c, secret.Keyring{}); err == nil {
		t.Fatal("missing secret keyring accepted")
	}
	if (DownloadKeyring{}).Validate() == nil {
		t.Fatal("zero keyring valid")
	}
}

func TestDownloadKeyringNoSensitiveProjection(t *testing.T) {
	k := downloadTestKeys(t)
	var output bytes.Buffer
	for _, value := range []any{k, &k, struct{ ring DownloadKeyring }{k}, struct{ any any }{k}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			fmt.Fprintf(&output, format, value)
		}
		b, _ := json.Marshal(value)
		output.Write(b)
		slog.New(slog.NewTextHandler(&output, nil)).Info("ring", "ring", value)
		slog.New(slog.NewJSONHandler(&output, nil)).Info("ring", "ring", value)
	}
	for _, b := range []byte{5, 6} {
		if strings.Contains(output.String(), downloadTestMaterial(b)) || strings.Contains(output.String(), string(bytes.Repeat([]byte{b}, 32))) {
			t.Fatal("material escaped projection")
		}
	}
	if err := json.Unmarshal([]byte(`"download_keyring"`), &k); err == nil {
		t.Fatal("JSON manufactured keyring")
	}
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			for range 100 {
				key, ok := k.key("old")
				if !ok || key[0] != 5 {
					t.Error("rotation lookup")
				}
				key[0] = 7
			}
		})
	}
	group.Wait()
	key, _ := k.key("old")
	if key[0] != 5 {
		t.Fatal("returned key was not copied")
	}
}
