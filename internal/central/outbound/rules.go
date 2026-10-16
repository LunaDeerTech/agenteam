package outbound

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"slices"
	"strings"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
)

type portData struct {
	all      bool
	selected []uint16
}
type Ports struct{ data func() portData }

func AllPorts() Ports { return Ports{data: func() portData { return portData{all: true} }} }
func SelectedPorts(ports ...uint16) (Ports, error) {
	if len(ports) == 0 || len(ports) > 256 {
		return Ports{}, invalid()
	}
	p := slices.Clone(ports)
	slices.Sort(p)
	if p[0] == 0 || len(slices.Compact(slices.Clone(p))) != len(p) {
		return Ports{}, invalid()
	}
	return Ports{data: func() portData { return portData{selected: p} }}, nil
}

type RuleMode string

const (
	Allowlist RuleMode = "allowlist"
	Denylist  RuleMode = "denylist"
)

func (m RuleMode) Valid() bool { return m == Allowlist || m == Denylist }

type ruleData struct {
	target   string
	prefix   netip.Prefix
	domain   string
	wildcard bool
	ports    Ports
}
type Rule struct{ data func() ruleData }

func NewRule(target string, ports Ports) (Rule, error) {
	if ports.data == nil {
		return Rule{}, invalid()
	}
	d := ruleData{ports: ports}
	if ip, err := netip.ParseAddr(target); err == nil {
		if ip.Is4In6() || ip.Zone() != "" {
			return Rule{}, invalid()
		}
		d.prefix = netip.PrefixFrom(ip, ip.BitLen())
		d.target = d.prefix.String()
	} else if strings.Contains(target, "/") {
		prefix, err := netip.ParsePrefix(target)
		if err != nil || prefix != prefix.Masked() || prefix.String() != target || prefix.Addr().Is4In6() || prefix.Addr().Zone() != "" {
			return Rule{}, invalid()
		}
		d.prefix, d.target = prefix, prefix.String()
	} else {
		d.wildcard = strings.HasPrefix(target, "*.")
		host := target
		if d.wildcard {
			host = strings.TrimPrefix(host, "*.")
		}
		canonical, err := normalizeHost(host)
		if err != nil {
			return Rule{}, invalid()
		}
		if _, err = netip.ParseAddr(canonical); err == nil {
			return Rule{}, invalid()
		}
		d.domain, d.target = canonical, canonical
		if d.wildcard {
			d.target = "*." + canonical
		}
	}
	return Rule{data: func() ruleData { return d }}, nil
}

type Rules struct{ data func() []Rule }
type ruleWire struct {
	Target string          `json:"target"`
	Ports  json.RawMessage `json:"ports"`
}

func ruleJSON(rule Rule) []byte {
	d := rule.data()
	p := d.ports.data()
	var raw []byte
	if p.all {
		raw = []byte(`"all"`)
	} else {
		raw, _ = json.Marshal(p.selected)
	}
	b, _ := json.Marshal(ruleWire{d.target, raw})
	return b
}
func NewRules(rules ...Rule) (Rules, error) {
	if len(rules) > 512 {
		return Rules{}, invalid()
	}
	r := slices.Clone(rules)
	for _, rule := range r {
		if rule.data == nil {
			return Rules{}, invalid()
		}
	}
	slices.SortFunc(r, func(a, b Rule) int { return bytes.Compare(ruleJSON(a), ruleJSON(b)) })
	for i := 1; i < len(r); i++ {
		if bytes.Equal(ruleJSON(r[i-1]), ruleJSON(r[i])) {
			return Rules{}, invalid()
		}
	}
	return Rules{data: func() []Rule { return r }}, nil
}
func (r Rules) Valid() bool { return r.data != nil }
func (r Rules) Count() int {
	if !r.Valid() {
		return 0
	}
	return len(r.data())
}

// JSON is an explicit, canonical administrator/storage projection. The ordinary
// fmt, slog and JSON projections do not disclose configured private networks.
func (r Rules) JSON() []byte {
	if !r.Valid() {
		return nil
	}
	w := make([]json.RawMessage, 0, r.Count())
	for _, rule := range r.data() {
		w = append(w, ruleJSON(rule))
	}
	b, _ := json.Marshal(w)
	return b
}
func (r Rules) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbound_rules") }
func (r Rules) MarshalJSON() ([]byte, error) { return []byte(`"outbound_rules"`), nil }
func (r Rules) LogValue() slog.Value         { return slog.StringValue("outbound_rules") }
func (r Rule) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "outbound_rule") }
func (r Rule) MarshalJSON() ([]byte, error)  { return []byte(`"outbound_rule"`), nil }
func (r Rule) LogValue() slog.Value          { return slog.StringValue("outbound_rule") }
func (p Ports) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbound_ports") }
func (p Ports) MarshalJSON() ([]byte, error) { return []byte(`"outbound_ports"`), nil }
func (p Ports) LogValue() slog.Value         { return slog.StringValue("outbound_ports") }

func DecodeRules(raw []byte) (Rules, error) {
	if len(raw) > 1<<20 || !strictJSON(raw) {
		return Rules{}, invalid()
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil || len(values) > 512 {
		return Rules{}, invalid()
	}
	rules := make([]Rule, 0, len(values))
	for _, v := range values {
		var fields map[string]json.RawMessage
		if json.Unmarshal(v, &fields) != nil || len(fields) != 2 || fields["target"] == nil || fields["ports"] == nil {
			return Rules{}, invalid()
		}
		for k, v := range fields {
			if k != "target" && k != "ports" || bytes.Equal(v, []byte("null")) {
				return Rules{}, invalid()
			}
		}
		var w ruleWire
		d := json.NewDecoder(bytes.NewReader(v))
		d.DisallowUnknownFields()
		if d.Decode(&w) != nil {
			return Rules{}, invalid()
		}
		var p Ports
		if bytes.Equal(bytes.TrimSpace(w.Ports), []byte(`"all"`)) {
			p = AllPorts()
		} else {
			var selected []uint16
			if json.Unmarshal(w.Ports, &selected) != nil {
				return Rules{}, invalid()
			}
			var e error
			p, e = SelectedPorts(selected...)
			if e != nil {
				return Rules{}, e
			}
		}
		r, e := NewRule(w.Target, p)
		if e != nil {
			return Rules{}, e
		}
		rules = append(rules, r)
	}
	return NewRules(rules...)
}

// strictJSON rejects duplicate keys at every depth, nulls and trailing values.
func strictJSON(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 8 {
			return false
		}
		t, e := d.Token()
		if e != nil || t == nil {
			return false
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				s, ok := key.(string)
				if e != nil || !ok || seen[s] {
					return false
				}
				seen[s] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim(']')
		}
		return false
	}
	if !value(0) {
		return false
	}
	_, e := d.Token()
	return e == io.EOF
}

// check admits the complete resolved set against one current policy snapshot.
// Address-risk defaults are ordinary editable rules. A denied address cannot
// be discarded in favor of another DNS result.
func (r Rules) check(mode RuleMode, host string, addresses []netip.Addr, port uint16) ac.Reason {
	if !mode.Valid() || !r.Valid() {
		return ac.PolicyUnavailable
	}
	if port == 0 || len(addresses) == 0 || len(addresses) > 64 {
		return ac.InvalidTarget
	}
	literal := false
	if _, err := netip.ParseAddr(host); err == nil {
		literal = true
	} else {
		canonical, err := normalizeHost(host)
		if err != nil {
			return ac.InvalidTarget
		}
		host = canonical
	}
	for _, ip := range addresses {
		if !ip.IsValid() || ip.Zone() != "" {
			return ac.InvalidTarget
		}
	}
	rules := r.data()
	matchesPort := func(d ruleData) bool {
		p := d.ports.data()
		return p.all || slices.Contains(p.selected, port)
	}
	if !literal {
		for _, rule := range rules {
			d := rule.data()
			if d.domain == "" || !matchesPort(d) {
				continue
			}
			match := host == d.domain
			if d.wildcard {
				match = host != d.domain && strings.HasSuffix(host, "."+d.domain)
			}
			if match {
				if mode == Denylist {
					return ac.RuleNotAllowed
				}
				return ""
			}
		}
	}
	for _, ip := range addresses {
		matched := false
		for _, rule := range rules {
			d := rule.data()
			if d.prefix.IsValid() && d.prefix.Contains(ip.Unmap()) && matchesPort(d) {
				matched = true
				break
			}
		}
		if mode == Allowlist && !matched || mode == Denylist && matched {
			return ac.RuleNotAllowed
		}
	}
	return ""
}
