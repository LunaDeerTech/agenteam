package account

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"testing"
)

func TestCanonicalIdentityValidation(t *testing.T) {
	v, e := NormalizeEmail("A.B+tag@EXAMPLE.COM")
	if e != nil || v != "a.b+tag@example.com" {
		t.Fatal(v, e)
	}
	for _, v := range []string{" User@example.com", "User@example.com ", "Display <user@example.com>", "é@example.com", "user@例子.test", "user@example.com\n"} {
		if _, e := NormalizeEmail(v); e == nil {
			t.Fatal("unsafe email")
		}
	}
	for _, v := range []string{"one-two", "abc", "012"} {
		if _, e := NormalizeUsername(v); e != nil {
			t.Fatal(e)
		}
	}
	for _, v := range []string{"admin", "root", "Admin", "-user", "user-", "aa", "a_b", "用户abc"} {
		if _, e := NormalizeUsername(v); e == nil {
			t.Fatal("username accepted")
		}
	}
	if e := ValidateDisplayName("  中文名称  "); e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"name\n", "name\u0085", strings.Repeat("界", 81), "\xff"} {
		if e := ValidateDisplayName(v); e == nil {
			t.Fatal("display accepted")
		}
	}
}

func TestNormalizeEmailCanonicalReentry(t *testing.T) {
	canonical, err := NormalizeEmail("A@[IPv6:2001:DB8::7]")
	if err != nil || canonical != "a@[ipv6:2001:db8::7]" {
		t.Fatal("old legal input changed canonical")
	}
	again, err := NormalizeEmail(canonical)
	if err != nil || again != canonical {
		t.Fatal("canonical email cannot be normalized again")
	}
}

// These fixed outputs are also the email bytes consumed by existing identity,
// privacy and command semantic/MAC inputs. No IP spelling is recompressed.
func TestNormalizeEmailOldGoldenAndClosure(t *testing.T) {
	cases := []struct{ name, input, canonical string }{
		{"ordinary", "A.B+tag@EXAMPLE.COM", "a.b+tag@example.com"},
		{"domain-punctuation", "A@domain!TEST", "a@domain!test"},
		{"domain-underscore", "A@domain_TEST", "a@domain_test"},
		{"local-atoms", "A!#$%&'*+-/=?^_`{|}~@EXAMPLE.test", "a!#$%&'*+-/=?^_`{|}~@example.test"},
		{"ipv4", "A@[192.0.2.7]", "a@[192.0.2.7]"},
		{"ipv6", "A@[IPv6:2001:DB8::7]", "a@[ipv6:2001:db8::7]"},
		{"expanded", "A@[IPv6:2001:0DB8:0000:0000:0000:0000:0000:0007]", "a@[ipv6:2001:0db8:0000:0000:0000:0000:0000:0007]"},
		{"prefixed-ipv4", "A@[IPv6:192.0.2.7]", "a@[ipv6:192.0.2.7]"},
		{"prefixed-mapped", "A@[IPv6:::FFFF:192.0.2.7]", "a@[ipv6:::ffff:192.0.2.7]"},
		{"unprefixed-mapped", "A@[::FFFF:192.0.2.7]", "a@[::ffff:192.0.2.7]"},
		{"unprefixed-expanded-mapped", "A@[0:0:0:0:0:FFFF:C000:207]", "a@[0:0:0:0:0:ffff:c000:207]"},
		{"254-bytes", "A@" + strings.Repeat("B", 252), "a@" + strings.Repeat("b", 252)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeEmail(tc.input)
			if err != nil || got != tc.canonical {
				t.Fatal("old canonical bytes changed")
			}
			for range 3 {
				got, err = NormalizeEmail(got)
				if err != nil || got != tc.canonical {
					t.Fatal("canonical closure failed")
				}
			}
		})
	}
}

func TestNormalizeEmailRejectsOutsideCanonicalClosure(t *testing.T) {
	inputs := []string{
		"", "a@" + strings.Repeat("b", 253), "a@[iPv6:2001:db8::7]", "a@[IPV6:2001:db8::7]",
		"A@[ipv6:2001:db8::7]", "a@[ipv6:2001:DB8::7]", "a@[2001:db8::7]",
		"a@[IPv6:fe80::1%lo]", "a@[ipv6:fe80::1%lo]", "a@[192.000.2.7]", "a@[1.2.3.999]",
		"a@[ipv6:not-an-ip]", "a@[ipv6:2001:db8::7]tail", "a@[ipv6:2001:db8::7", "a@[ipv6:@2001:db8::7]",
		"@[ipv6:2001:db8::7]", "a@b@[ipv6:2001:db8::7]", "a..b@example.test", "a@b..c",
		"display <a@example.test>", "a(comment)@example.test", "a@example.test(comment)",
		"a@example.test,b@example.test", "group:a@example.test;", "\"a\"@example.test", "a\\b@example.test",
		" a@example.test", "a@example.test ", "é@example.test", "a@例子.test", string([]byte{'a', 0xff, '@', 'b'}),
	}
	for b := 0; b <= 255; b++ {
		if b < 33 || b > 126 {
			inputs = append(inputs, "a"+string([]byte{byte(b)})+"@example.test")
		}
	}
	for i, input := range inputs {
		t.Run(fmt.Sprintf("rejected-%03d", i), func(t *testing.T) {
			got, err := NormalizeEmail(input)
			var fault *foundation.Fault
			if got != "" || !errors.As(err, &fault) || fault.Code != foundation.InvalidArgument {
				t.Fatal("unsafe email admitted or error changed")
			}
		})
	}
}
