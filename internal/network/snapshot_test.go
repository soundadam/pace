package network

import (
	"reflect"
	"testing"
)

// The fixtures below follow the shape each tool really prints. None were
// captured from a live host; cases marked "invented" describe a shape that was
// reasoned about rather than observed.

func TestParseDarwinRoute(t *testing.T) {
	tests := []struct {
		name          string
		output        string
		wantInterface string
		wantGateway   string
	}{
		{
			name: "typical Wi-Fi default route",
			output: `   route to: default
destination: default
       mask: default
    gateway: 192.168.1.1
  interface: en0
      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>
 recvpipe  sendpipe  ssthresh  rtt,msec    rttvar  hopcount      mtu     expire
       0         0         0         0         0         0      1500         0
`,
			wantInterface: "en0",
			wantGateway:   "192.168.1.1",
		},
		{
			name:          "ragged indentation and trailing spaces",
			output:        "\n\tgateway:   10.0.0.1   \n        interface:\ten0  \n\n",
			wantInterface: "en0",
			wantGateway:   "10.0.0.1",
		},
		{
			name: "IPv6 link-local gateway keeps its zone",
			output: `   route to: default
    gateway: fe80::1%en0
  interface: en0
`,
			wantInterface: "en0",
			wantGateway:   "fe80::1%en0",
		},
		{
			name: "VPN tunnel with no gateway line",
			output: `   route to: default
  interface: utun4
      flags: <UP,DONE,WASCLONED,IFSCOPE>
`,
			wantInterface: "utun4",
		},
		{
			name:   "no default route",
			output: "   route to: default\nroute: writing to routing socket: not in table\n",
		},
		{name: "empty output"},
		{name: "no colons at all", output: "nothing here\nstill nothing\n"},
		{
			// Invented: covers a key present with an empty value.
			name:   "keys with empty values",
			output: "  interface:\n    gateway:\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotInterface, gotGateway := parseDarwinRoute(test.output)
			if gotInterface != test.wantInterface || gotGateway != test.wantGateway {
				t.Errorf("parseDarwinRoute() = %q/%q, want %q/%q",
					gotInterface, gotGateway, test.wantInterface, test.wantGateway)
			}
		})
	}
}

func TestParseLinuxRoute(t *testing.T) {
	tests := []struct {
		name          string
		output        string
		wantInterface string
		wantGateway   string
	}{
		{
			name:          "dhcp default route",
			output:        "default via 10.0.0.1 dev eth0 proto dhcp src 10.0.0.42 metric 100\n",
			wantInterface: "eth0",
			wantGateway:   "10.0.0.1",
		},
		{
			name:          "predictable interface name with extra whitespace",
			output:        "   default   via   192.168.1.1   dev   enp3s0   proto   static   \n",
			wantInterface: "enp3s0",
			wantGateway:   "192.168.1.1",
		},
		{
			name:          "IPv6 default route",
			output:        "default via fe80::1 dev wlan0 proto ra metric 1024 expires 1799sec hoplimit 64 pref medium\n",
			wantInterface: "wlan0",
			wantGateway:   "fe80::1",
		},
		{
			name:          "point-to-point link has a device but no gateway",
			output:        "default dev wg0 scope link\n",
			wantInterface: "wg0",
		},
		{
			// Invented: ip(8) prints one route per line when several exist.
			// The parser keeps the last "dev"/"via" it sees.
			name:          "multiple default routes keep the last one",
			output:        "default via 10.0.0.1 dev eth0 metric 100\ndefault via 192.168.1.1 dev wlan0 metric 600\n",
			wantInterface: "wlan0",
			wantGateway:   "192.168.1.1",
		},
		{name: "no default route prints nothing"},
		{
			// Invented: a truncated line must not index past the end. The
			// gateway is still learned; only the dangling "dev" is ignored.
			name:        "trailing keyword with no value",
			output:      "default via 10.0.0.1 dev",
			wantGateway: "10.0.0.1",
		},
		{name: "only the keyword", output: "via"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotInterface, gotGateway := parseLinuxRoute(test.output)
			if gotInterface != test.wantInterface || gotGateway != test.wantGateway {
				t.Errorf("parseLinuxRoute() = %q/%q, want %q/%q",
					gotInterface, gotGateway, test.wantInterface, test.wantGateway)
			}
		})
	}
}

func TestParseWindowsRoute(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantAddress string
		wantGateway string
	}{
		{
			name: "typical route table",
			output: `
IPv4 Route Table
===========================================================================
Active Routes:
Network Destination        Netmask          Gateway       Interface  Metric
          0.0.0.0          0.0.0.0      192.168.1.1     192.168.1.23     25
    192.168.1.0    255.255.255.0         On-link       192.168.1.23    281
===========================================================================
`,
			wantAddress: "192.168.1.23",
			wantGateway: "192.168.1.1",
		},
		{
			// Some VPN clients publish an on-link default route. The parser
			// passes "On-link" through verbatim as the gateway; see the note
			// in the package's test report.
			name:        "on-link default route yields a non-address gateway",
			output:      "          0.0.0.0          0.0.0.0         On-link       10.8.0.2     25\n",
			wantAddress: "10.8.0.2",
			wantGateway: "On-link",
		},
		{
			name:        "the first default route wins",
			output:      "0.0.0.0 0.0.0.0 192.168.1.1 192.168.1.23 25\n0.0.0.0 0.0.0.0 10.0.0.1 10.0.0.5 50\n",
			wantAddress: "192.168.1.23",
			wantGateway: "192.168.1.1",
		},
		{name: "no default route in the table", output: "    192.168.1.0    255.255.255.0         On-link       192.168.1.23    281\n"},
		{name: "empty output"},
		{
			// Invented: a truncated row must be skipped, not indexed into.
			name:   "default route row with too few columns",
			output: "0.0.0.0 0.0.0.0 192.168.1.1\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotAddress, gotGateway := parseWindowsRoute(test.output)
			if gotAddress != test.wantAddress || gotGateway != test.wantGateway {
				t.Errorf("parseWindowsRoute() = %q/%q, want %q/%q",
					gotAddress, gotGateway, test.wantAddress, test.wantGateway)
			}
		})
	}
}

func TestParseDarwinDNS(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name: "several resolvers with duplicates",
			output: `DNS configuration

resolver #1
  search domain[0] : lan
  nameserver[0] : 192.168.1.1
  nameserver[1] : 1.1.1.1
  if_index : 14 (en0)

resolver #2
  domain   : local
  nameserver[0] : 192.168.1.1
`,
			want: []string{"1.1.1.1", "192.168.1.1"},
		},
		{
			name:   "IPv6 nameservers keep every colon after the first",
			output: "  nameserver[0] : 2606:4700:4700::1111\n  nameserver[1] : 2606:4700:4700::1001\n",
			want:   []string{"2606:4700:4700::1001", "2606:4700:4700::1111"},
		},
		{
			name:   "ragged spacing around the separator",
			output: "nameserver[0]:8.8.8.8\n      nameserver[10]      :      8.8.4.4     \n",
			want:   []string{"8.8.4.4", "8.8.8.8"},
		},
		{
			name:   "non-nameserver lines are ignored",
			output: "resolver #1\n  search domain[0] : 1.2.3.4\n  options : ndots:5\n",
			want:   []string{},
		},
		{
			name:   "malformed values are dropped",
			output: "  nameserver[0] : not-an-ip\n  nameserver[1] :\n  nameserver[2] : 9.9.9.9\n",
			want:   []string{"9.9.9.9"},
		},
		{
			// Invented: a nameserver line with no colon at all.
			name:   "nameserver line with no separator",
			output: "  nameserver[0] 1.1.1.1\n",
			want:   []string{},
		},
		{name: "empty output", want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseDarwinDNS(test.output); !reflect.DeepEqual(got, test.want) {
				t.Errorf("parseDarwinDNS() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseResolvConf(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "NetworkManager style file",
			output: "# Generated by NetworkManager\nsearch lan\nnameserver 192.168.1.1\nnameserver 1.1.1.1\noptions edns0 trust-ad\n",
			want:   []string{"1.1.1.1", "192.168.1.1"},
		},
		{
			name:   "systemd-resolved stub",
			output: "nameserver 127.0.0.53\noptions edns0\n",
			want:   []string{"127.0.0.53"},
		},
		{
			name:   "IPv6-only resolver",
			output: "nameserver 2606:4700:4700::1111\nnameserver 2606:4700:4700::1001\n",
			want:   []string{"2606:4700:4700::1001", "2606:4700:4700::1111"},
		},
		{
			name:   "tabs and repeated servers",
			output: "nameserver\t8.8.8.8\nnameserver 8.8.8.8\n\n   nameserver   8.8.4.4\n",
			want:   []string{"8.8.4.4", "8.8.8.8"},
		},
		{
			name:   "invalid and bare directives are dropped",
			output: "nameserver invalid\nnameserver\nsearch 1.1.1.1\nnameserver 9.9.9.9\n",
			want:   []string{"9.9.9.9"},
		},
		{
			// Invented: a commented-out server must not be picked up.
			name:   "commented out server is ignored",
			output: "#nameserver 1.1.1.1\n# nameserver 8.8.8.8\n",
			want:   []string{},
		},
		{name: "empty file", want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseResolvConf(test.output); !reflect.DeepEqual(got, test.want) {
				t.Errorf("parseResolvConf() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseWindowsDNS(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "one server on the label line",
			output: "   DNS Servers . . . . . . . . . . . : 192.168.1.1\n",
			want:   []string{"192.168.1.1"},
		},
		{
			name:   "continuation lines are collected",
			output: "   DNS Servers . . . . . . . . . . . : 192.168.1.1\n                                       1.1.1.1\n                                       8.8.8.8\n",
			want:   []string{"1.1.1.1", "192.168.1.1", "8.8.8.8"},
		},
		{
			// Regression: an IPv6 server on a continuation line contains
			// colons and used to be mistaken for a new "label: value" line,
			// which both dropped it and ended the block early.
			name: "IPv6 servers on continuation lines are kept",
			output: `   DNS Servers . . . . . . . . . . . : 192.168.1.1
                                       2606:4700:4700::1111
                                       2606:4700:4700::1001
   NetBIOS over Tcpip. . . . . . . . : Enabled
`,
			want: []string{"192.168.1.1", "2606:4700:4700::1001", "2606:4700:4700::1111"},
		},
		{
			name:   "an IPv6 server on the label line is kept",
			output: "   DNS Servers . . . . . . . . . . . : 2606:4700:4700::1111\n",
			want:   []string{"2606:4700:4700::1111"},
		},
		{
			name: "a following label ends the block",
			output: `   DNS Servers . . . . . . . . . . . : 8.8.8.8
   NetBIOS over Tcpip. . . . . . . . : Enabled
   Lease Obtained. . . . . . . . . . : 1.1.1.1
`,
			want: []string{"8.8.8.8"},
		},
		{
			name: "a non-address line with no colon ends the block",
			output: `   DNS Servers . . . . . . . . . . . : 8.8.8.8
                                       8.8.4.4
Ethernet adapter Ethernet 2
                                       1.1.1.1
`,
			want: []string{"8.8.4.4", "8.8.8.8"},
		},
		{
			name: "two adapters both contribute",
			output: `Ethernet adapter Ethernet:
   DNS Servers . . . . . . . . . . . : 10.0.0.1

Wireless LAN adapter Wi-Fi:
   DNS Servers . . . . . . . . . . . : 192.168.1.1
                                       1.1.1.1
`,
			want: []string{"1.1.1.1", "10.0.0.1", "192.168.1.1"},
		},
		{
			// Known gap: ipconfig prints Windows' default link-local
			// resolvers with a zone index, which net.ParseIP rejects, so they
			// are dropped rather than reported.
			name:   "zone-suffixed servers are dropped",
			output: "   DNS Servers . . . . . . . . . . . : fec0:0:0:ffff::1%1\n",
			want:   []string{},
		},
		{
			name:   "singular DNS Server label is also recognised",
			output: "   DNS Server . . . . . . . . . . . . : 1.0.0.1\n",
			want:   []string{"1.0.0.1"},
		},
		{
			name:   "an empty DNS Servers value contributes nothing",
			output: "   DNS Servers . . . . . . . . . . . :\n",
			want:   []string{},
		},
		{
			name:   "output with no DNS section",
			output: "Windows IP Configuration\n\n   Host Name . . . . . . . . . . . . : example\n",
			want:   []string{},
		},
		{name: "empty output", want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseWindowsDNS(test.output); !reflect.DeepEqual(got, test.want) {
				t.Errorf("parseWindowsDNS() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseMacHardwarePort(t *testing.T) {
	const output = `Hardware Port: Ethernet Adapter (en5)
Device: en5
Ethernet Address: 00:00:5e:00:53:05

Hardware Port: Wi-Fi
Device: en0
Ethernet Address: 00:00:5e:00:53:00

Hardware Port: Thunderbolt Bridge
Device: bridge0
Ethernet Address: N/A

VLAN Configurations
===
`
	tests := []struct {
		name            string
		output          string
		activeInterface string
		want            string
	}{
		{name: "wifi device", output: output, activeInterface: "en0", want: "Wi-Fi"},
		{name: "ethernet device", output: output, activeInterface: "en5", want: "Ethernet Adapter (en5)"},
		{name: "bridge device", output: output, activeInterface: "bridge0", want: "Thunderbolt Bridge"},
		{name: "device networksetup does not list", output: output, activeInterface: "utun4"},
		{name: "empty interface name matches nothing", output: output},
		{name: "empty output", activeInterface: "en0"},
		{
			// Invented: a Device line before any Hardware Port line must
			// return the empty port rather than the previous entry's.
			name:            "device with no preceding hardware port",
			output:          "Device: en0\n",
			activeInterface: "en0",
		},
		{
			name:            "ragged spacing still matches",
			output:          "Hardware Port:   Wi-Fi  \n   Device:\ten0  \n",
			activeInterface: "en0",
			want:            "Wi-Fi",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseMacHardwarePort(test.output, test.activeInterface); got != test.want {
				t.Errorf("parseMacHardwarePort(_, %q) = %q, want %q", test.activeInterface, got, test.want)
			}
		})
	}
}

func TestClassifyHardwarePort(t *testing.T) {
	tests := []struct {
		port string
		want string
	}{
		// Names networksetup really prints.
		{"Wi-Fi", "wifi"},
		{"AirPort", "wifi"},
		{"Ethernet", "ethernet"},
		{"USB 10/100/1000 LAN", "other"},
		{"Thunderbolt Ethernet Slot 1", "ethernet"},
		{"Thunderbolt Bridge", "ethernet"},
		{"iPhone USB", "other"},
		{"Bluetooth PAN", "other"},
		// Matching is case-insensitive and substring-based.
		{"wi-fi", "wifi"},
		{"WIFI", "wifi"},
		{"wifi", "wifi"},
		{"Apple Wi-Fi Adapter", "wifi"},
		{"ETHERNET ADAPTER (EN5)", "ethernet"},
		// Invented: names a VPN client might register.
		{"Example VPN", "tunnel"},
		{"Tunnel Interface", "tunnel"},
		{"vpn", "tunnel"},
		// Wi-Fi wins over ethernet when a name contains both, because the
		// wifi case is listed first.
		{"Wi-Fi Ethernet Bridge", "wifi"},
		// Input it must not recognise.
		{"", "other"},
		{"   ", "other"},
		{"Loopback", "other"},
		{"wi fi", "other"},
		{"ether", "other"},
		{"Thunder", "other"},
	}
	for _, test := range tests {
		t.Run(test.port, func(t *testing.T) {
			if got := classifyHardwarePort(test.port); got != test.want {
				t.Errorf("classifyHardwarePort(%q) = %q, want %q", test.port, got, test.want)
			}
		})
	}
}

func TestClassifyInterface(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"", ""},
		{"utun4", "tunnel"},
		{"tun0", "tunnel"},
		{"tap0", "tunnel"},
		{"wg0", "tunnel"},
		{"eth0", "ethernet"},
		{"eno1", "ethernet"},
		{"enp3s0", "ethernet"},
		{"wlan0", "wifi"},
		{"wlp2s0", "wifi"},
		// macOS names carry no kind, so they fall through to the hardware
		// port lookup.
		{"en0", "other"},
		{"bridge0", "other"},
		{"lo0", "other"},
		{"ppp0", "other"},
		// Prefix matching only: a kind in the middle of a name is not enough.
		{"veth0", "other"},
		{"mywlan", "other"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyInterface(test.name); got != test.want {
				t.Errorf("classifyInterface(%q) = %q, want %q", test.name, got, test.want)
			}
		})
	}
}

func TestAppendValidIP(t *testing.T) {
	tests := []struct {
		name  string
		start []string
		raw   string
		want  []string
	}{
		{name: "plain IPv4", raw: "1.1.1.1", want: []string{"1.1.1.1"}},
		{name: "whitespace is trimmed", raw: "  1.1.1.1\t", want: []string{"1.1.1.1"}},
		{name: "IPv6 is normalised", raw: "2606:4700:4700:0000::1111", want: []string{"2606:4700:4700::1111"}},
		{name: "invalid input is dropped", start: []string{"1.1.1.1"}, raw: "nope", want: []string{"1.1.1.1"}},
		{name: "empty input is dropped", raw: ""},
		{name: "CIDR is not an address", raw: "10.0.0.0/8"},
		{name: "zone suffix is rejected", raw: "fe80::1%en0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := appendValidIP(test.start, test.raw)
			if len(got) == 0 && len(test.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("appendValidIP(%#v, %q) = %#v, want %#v", test.start, test.raw, got, test.want)
			}
		})
	}
}

func TestUniqueSorted(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   []string
	}{
		{name: "nil input yields an empty slice", want: []string{}},
		{name: "sorts", values: []string{"b", "a"}, want: []string{"a", "b"}},
		{name: "deduplicates", values: []string{"a", "a", "a"}, want: []string{"a"}},
		{
			name:   "sorts lexically, not numerically",
			values: []string{"192.168.1.1", "10.0.0.1", "8.8.8.8"},
			want:   []string{"10.0.0.1", "192.168.1.1", "8.8.8.8"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := uniqueSorted(test.values); !reflect.DeepEqual(got, test.want) {
				t.Errorf("uniqueSorted(%#v) = %#v, want %#v", test.values, got, test.want)
			}
		})
	}
}
