// Package outbound owns runtime target validation, policy publication and all
// controlled network sends. Deployment database connections are separate.
package outbound

import "net/netip"

type AddressClass string

const (
	Public      AddressClass = "public"
	Private     AddressClass = "private"
	Loopback    AddressClass = "loopback"
	LinkLocal   AddressClass = "link_local"
	Multicast   AddressClass = "multicast"
	Unspecified AddressClass = "unspecified"
	Reserved    AddressClass = "reserved"
	Metadata    AddressClass = "metadata"
)

// These are deliberately conservative whole-prefix exclusions, including the
// globally reachable exceptions within IANA special-purpose registrations.
// Sources: https://www.iana.org/assignments/iana-ipv4-special-registry/
// and https://www.iana.org/assignments/iana-ipv6-special-registry/ . Do not replace
// this table with IsGlobalUnicast, which also accepts special-use addresses.
var reserved4 = prefixes("0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.31.196.0/24", "192.52.193.0/24", "192.88.99.0/24", "192.175.48.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4")
var reserved6 = prefixes("::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16", "2620:4f:8000::/48", "3fff::/20")
var privateRanges = prefixes("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7")
var public6 = netip.MustParsePrefix("2000::/3")

// AWS EC2/ECS, Google Compute Engine and Azure platform endpoints:
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-metadata-endpoint.html
// https://cloud.google.com/compute/docs/metadata/overview
// https://learn.microsoft.com/azure/virtual-network/what-is-ip-address-168-63-129-16
// Alibaba metadata: https://www.alibabacloud.com/help/en/ecs/user-guide/view-instance-metadata
var metadataAddresses = []netip.Addr{
	netip.MustParseAddr("169.254.169.254"), netip.MustParseAddr("169.254.170.2"),
	netip.MustParseAddr("100.100.100.200"), netip.MustParseAddr("168.63.129.16"),
	netip.MustParseAddr("fd00:ec2::254"),
}

func prefixes(values ...string) []netip.Prefix {
	result := make([]netip.Prefix, len(values))
	for i, value := range values {
		result[i] = netip.MustParsePrefix(value)
	}
	return result
}
func contains(ranges []netip.Prefix, ip netip.Addr) bool {
	for _, p := range ranges {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Classify always unmapps IPv4-in-IPv6 before classification. Zone-qualified
// and invalid addresses are never approved, even if the underlying IP is public.
func Classify(ip netip.Addr) AddressClass {
	if !ip.IsValid() || ip.Zone() != "" {
		return Reserved
	}
	ip = ip.Unmap()
	for _, m := range metadataAddresses {
		if ip == m {
			return Metadata
		}
	}
	if ip.IsUnspecified() {
		return Unspecified
	}
	if ip.IsLoopback() {
		return Loopback
	}
	if ip.IsLinkLocalUnicast() {
		return LinkLocal
	}
	if ip.IsMulticast() {
		return Multicast
	}
	if contains(privateRanges, ip) {
		return Private
	}
	if ip.Is4() {
		if contains(reserved4, ip) {
			return Reserved
		}
		return Public
	}
	if !public6.Contains(ip) || contains(reserved6, ip) {
		return Reserved
	}
	return Public
}

func metadataHost(host string) bool {
	return host == "metadata.google.internal" || host == "metadata.goog"
}
