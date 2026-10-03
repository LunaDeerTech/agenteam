package outbound

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"slices"

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

type ruleData struct {
	prefix netip.Prefix
	ports  Ports
	http   bool
}
type Rule struct{ data func() ruleData }

func NewRule(cidr string, ports Ports, allowHTTP bool) (Rule, error) {
	p, e := netip.ParsePrefix(cidr)
	if e != nil || p != p.Masked() || p.String() != cidr || p.Addr().Is4In6() || ports.data == nil {
		return Rule{}, invalid()
	}
	inside := false
	for _, outer := range privateRanges {
		if outer.Contains(p.Addr()) && p.Bits() >= outer.Bits() {
			inside = true
		}
	}
	if !inside {
		return Rule{}, invalid()
	}
	d := ruleData{p, ports, allowHTTP}
	return Rule{data: func() ruleData { return d }}, nil
}

type Rules struct{ data func() []Rule }
type ruleWire struct {
	CIDR      string          `json:"cidr"`
	Ports     json.RawMessage `json:"ports"`
	AllowHTTP bool            `json:"allow_http"`
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
	b, _ := json.Marshal(ruleWire{d.prefix.String(), raw, d.http})
	return b
}
func NewRules(rules ...Rule) (Rules, error) {
	if len(rules) > 256 {
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
	if len(raw) > 512*1024 || !strictJSON(raw) {
		return Rules{}, invalid()
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil || len(values) > 256 {
		return Rules{}, invalid()
	}
	rules := make([]Rule, 0, len(values))
	for _, v := range values {
		var fields map[string]json.RawMessage
		if json.Unmarshal(v, &fields) != nil || len(fields) < 2 || len(fields) > 3 || fields["cidr"] == nil || fields["ports"] == nil {
			return Rules{}, invalid()
		}
		for k, v := range fields {
			if k != "cidr" && k != "ports" && k != "allow_http" || bytes.Equal(v, []byte("null")) {
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
		r, e := NewRule(w.CIDR, p, w.AllowHTTP)
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

// check is used only with the complete resolved set, at each real admission.
// Overlapping rules are alternatives: CIDR, port and HTTP must all match one.
func (r Rules) check(ip netip.Addr, port uint16, http bool) ac.Reason {
	class := Classify(ip)
	if class == Public {
		return ""
	}
	if class != Private {
		return ac.AddressForbidden
	}
	if !r.Valid() {
		return ac.PolicyUnavailable
	}
	ip = ip.Unmap()
	reason := ac.PrivateNotAllowed
	for _, rule := range r.data() {
		d := rule.data()
		if !d.prefix.Contains(ip) {
			continue
		}
		if reason == ac.PrivateNotAllowed {
			reason = ac.PortDenied
		}
		p := d.ports.data()
		if !p.all && !slices.Contains(p.selected, port) {
			continue
		}
		if http && !d.http {
			reason = ac.HTTPDenied
			continue
		}
		return ""
	}
	return reason
}
