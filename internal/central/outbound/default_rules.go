package outbound

// DefaultRules is the visible, editable initial denylist. It preserves the
// previous address exclusions as ordinary rules, not an admission bypass.
// Clearing these rows permits those valid destinations under denylist mode.
// DNS aliases do not become rule identities: rules match the original host.
func DefaultRules() Rules {
	rules, err := DecodeRules([]byte(defaultRulesJSON))
	if err != nil {
		panic("invalid compiled outbound default rules")
	}
	return rules
}

const defaultRulesJSON = `[{"target":"0.0.0.0/8","ports":"all"},{"target":"100.64.0.0/10","ports":"all"},{"target":"127.0.0.0/8","ports":"all"},{"target":"168.63.129.16/32","ports":"all"},{"target":"169.254.0.0/16","ports":"all"},{"target":"192.0.0.0/24","ports":"all"},{"target":"192.0.2.0/24","ports":"all"},{"target":"192.31.196.0/24","ports":"all"},{"target":"192.52.193.0/24","ports":"all"},{"target":"192.88.99.0/24","ports":"all"},{"target":"192.175.48.0/24","ports":"all"},{"target":"198.18.0.0/15","ports":"all"},{"target":"198.51.100.0/24","ports":"all"},{"target":"203.0.113.0/24","ports":"all"},{"target":"224.0.0.0/4","ports":"all"},{"target":"240.0.0.0/4","ports":"all"},{"target":"::/3","ports":"all"},{"target":"2001::/23","ports":"all"},{"target":"2001:db8::/32","ports":"all"},{"target":"2002::/16","ports":"all"},{"target":"2620:4f:8000::/48","ports":"all"},{"target":"3fff::/20","ports":"all"},{"target":"4000::/2","ports":"all"},{"target":"8000::/2","ports":"all"},{"target":"c000::/3","ports":"all"},{"target":"e000::/4","ports":"all"},{"target":"f000::/5","ports":"all"},{"target":"f800::/6","ports":"all"},{"target":"fd00:ec2::254/128","ports":"all"},{"target":"fe00::/7","ports":"all"},{"target":"metadata.google.internal","ports":"all"},{"target":"metadata.goog","ports":"all"}]`
