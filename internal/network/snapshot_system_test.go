package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// The command output in this file is hand-written in the shape each tool
// really prints, but it was NOT captured from a live host: doing so would bake
// the developer's own SSID, addresses and DNS servers into the repository.
// Where a shape is unusual (localised text, IPv6-only, On-link gateways) the
// individual case says where the shape comes from.

// These tests swap package-level variables, so none of them may call
// t.Parallel(). The helpers below restore the previous value via t.Cleanup.

var (
	errNotInstalled = errors.New(`exec: "route": executable file not found in $PATH`)
	errExitStatus1  = errors.New("exit status 1")
)

// realSystemInterfaces keeps the production enumerator that TestMain replaces,
// so TestSystemInterfacesAdapter can still reach it.
var realSystemInterfaces = systemInterfaces

// TestMain pins the package's system seams to values that cannot reach the
// host. A test that wants real-looking data must opt in through a helper, so
// no test can accidentally execute a command, read the host's interface list
// or read the host's /etc/resolv.conf.
func TestMain(m *testing.M) {
	runCommand = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("network tests must not execute real commands")
	}
	systemInterfaces = func() ([]netInterface, error) {
		return nil, errors.New("network tests must not read the host interface list")
	}
	resolvConfPath = filepath.Join(os.TempDir(), "soundprobe-no-such-resolv.conf")
	os.Exit(m.Run())
}

// TestSystemInterfacesAdapter exercises the real enumerator, which every other
// test stubs out. Unlike runCommand's production body it starts no subprocess
// and sends no traffic: it is a local read of the kernel's interface table.
// The assertions stay loose because the host's interfaces are not ours to
// predict, and a sandbox may legitimately expose none at all.
func TestSystemInterfacesAdapter(t *testing.T) {
	interfaces, err := realSystemInterfaces()
	if err != nil {
		t.Skipf("host interface table unavailable: %v", err)
	}
	for _, networkInterface := range interfaces {
		if networkInterface.Name == "" {
			t.Errorf("interface with no name: %+v", networkInterface)
		}
		// Addresses must already be resolved; the consumers never call back
		// into the stdlib for them.
		for _, address := range networkInterface.Addresses {
			if address == nil {
				t.Errorf("%s has a nil address", networkInterface.Name)
			}
		}
	}
}

// commandResult is one canned reply from a stubbed system command.
type commandResult struct {
	output string
	err    error
}

// commands builds a runCommand stub. Keys are the command name optionally
// followed by its arguments, joined by spaces; an unmatched command reports
// "not found", which is what an absent tool really looks like.
func commands(responses map[string]commandResult) commandRunner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		full := strings.TrimSpace(name + " " + strings.Join(args, " "))
		for _, key := range []string{full, name} {
			if result, ok := responses[key]; ok {
				return []byte(result.output), result.err
			}
		}
		return nil, fmt.Errorf("exec: %q: executable file not found in $PATH", name)
	}
}

func withCommands(t *testing.T, responses map[string]commandResult) {
	t.Helper()
	withRunCommand(t, commands(responses))
}

func withRunCommand(t *testing.T, runner commandRunner) {
	t.Helper()
	original := runCommand
	runCommand = runner
	t.Cleanup(func() { runCommand = original })
}

func withOS(t *testing.T, goos string) {
	t.Helper()
	original := currentOS
	currentOS = goos
	t.Cleanup(func() { currentOS = original })
}

func withInterfaces(t *testing.T, interfaces []netInterface, err error) {
	t.Helper()
	original := systemInterfaces
	systemInterfaces = func() ([]netInterface, error) { return interfaces, err }
	t.Cleanup(func() { systemInterfaces = original })
}

func withResolvConf(t *testing.T, contents string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write resolv.conf: %v", err)
	}
	original := resolvConfPath
	resolvConfPath = path
	t.Cleanup(func() { resolvConfPath = original })
}

// stubAddr stands in for the net.Addr values net.Interface.Addrs() returns.
// Those are CIDR strings, sometimes carrying a %zone suffix on IPv6.
type stubAddr string

func (a stubAddr) Network() string { return "ip+net" }
func (a stubAddr) String() string  { return string(a) }

func addrs(values ...string) []net.Addr {
	result := make([]net.Addr, 0, len(values))
	for _, value := range values {
		result = append(result, stubAddr(value))
	}
	return result
}

const upInterface = net.FlagUp | net.FlagRunning

// darwinRouteOutput is the shape of `route -n get default` on macOS.
const darwinRouteOutput = `   route to: default
destination: default
       mask: default
    gateway: 192.168.1.1
  interface: en0
      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>
`

// darwinDNSOutput is the shape of `scutil --dns`, which repeats each resolver.
const darwinDNSOutput = `DNS configuration

resolver #1
  search domain[0] : lan
  nameserver[0] : 192.168.1.1
  nameserver[1] : 1.1.1.1
  if_index : 14 (en0)
  flags    : Request A records, Request AAAA records

resolver #2
  domain   : local
  nameserver[0] : 192.168.1.1
`

// darwinHardwarePortsOutput is the shape of
// `networksetup -listallhardwareports`.
const darwinHardwarePortsOutput = `Hardware Port: Ethernet Adapter (en5)
Device: en5
Ethernet Address: 00:00:5e:00:53:05

Hardware Port: Wi-Fi
Device: en0
Ethernet Address: 00:00:5e:00:53:00

VLAN Configurations
===
`

func TestSnapshotDarwin(t *testing.T) {
	withOS(t, "darwin")
	withCommands(t, map[string]commandResult{
		"sw_vers -productVersion":             {output: "15.3.1\n"},
		"route -n get default":                {output: darwinRouteOutput},
		"scutil --dns":                        {output: darwinDNSOutput},
		"networksetup -listallhardwareports":  {output: darwinHardwarePortsOutput},
		"networksetup -getairportnetwork en0": {output: "Current Wi-Fi Network: Example Network\n"},
	})
	withInterfaces(t, []netInterface{
		{Name: "lo0", Flags: upInterface | net.FlagLoopback, Addresses: addrs("127.0.0.1/8", "::1/128")},
		{Name: "en0", Flags: upInterface, Addresses: addrs("192.168.1.23/24", "2001:db8::23/64", "fe80::1%en0")},
		{Name: "en5", Flags: 0, Addresses: addrs("10.9.9.9/24")},
	}, nil)

	got := Snapshot()

	if got.OS != "macOS 15.3.1" {
		t.Errorf("OS = %q, want %q", got.OS, "macOS 15.3.1")
	}
	if got.Architecture != runtime.GOARCH {
		t.Errorf("Architecture = %q, want %q", got.Architecture, runtime.GOARCH)
	}
	assertPointer(t, "ActiveInterface", got.ActiveInterface, "en0")
	assertPointer(t, "DefaultGateway", got.DefaultGateway, "192.168.1.1")
	assertPointer(t, "InterfaceKind", got.InterfaceKind, "wifi")
	assertPointer(t, "SSID", got.SSID, "Example Network")
	// Only en0's addresses: lo0 is loopback and en5 is down.
	assertSlice(t, "LocalIPv4", got.LocalIPv4, []string{"192.168.1.23"})
	assertSlice(t, "LocalIPv6", got.LocalIPv6, []string{"2001:db8::23", "fe80::1"})
	assertSlice(t, "DNSServers", got.DNSServers, []string{"1.1.1.1", "192.168.1.1"})
	if got.BSSID != nil {
		t.Errorf("BSSID = %q, want nil (never populated)", *got.BSSID)
	}
}

func TestSnapshotLinux(t *testing.T) {
	withOS(t, "linux")
	withCommands(t, map[string]commandResult{
		"ip route show default": {output: "default via 10.0.0.1 dev eth0 proto dhcp src 10.0.0.42 metric 100\n"},
	})
	withResolvConf(t, "# Generated by NetworkManager\nnameserver 10.0.0.1\nnameserver 1.1.1.1\noptions edns0 trust-ad\n")
	withInterfaces(t, []netInterface{
		{Name: "lo", Flags: upInterface | net.FlagLoopback, Addresses: addrs("127.0.0.1/8")},
		{Name: "eth0", Flags: upInterface, Addresses: addrs("10.0.0.42/24", "2001:db8:1::42/64")},
	}, nil)

	got := Snapshot()

	if got.OS != "linux" {
		t.Errorf("OS = %q, want %q", got.OS, "linux")
	}
	assertPointer(t, "ActiveInterface", got.ActiveInterface, "eth0")
	assertPointer(t, "DefaultGateway", got.DefaultGateway, "10.0.0.1")
	assertPointer(t, "InterfaceKind", got.InterfaceKind, "ethernet")
	assertSlice(t, "LocalIPv4", got.LocalIPv4, []string{"10.0.0.42"})
	assertSlice(t, "LocalIPv6", got.LocalIPv6, []string{"2001:db8:1::42"})
	assertSlice(t, "DNSServers", got.DNSServers, []string{"1.1.1.1", "10.0.0.1"})
	if got.SSID != nil {
		t.Errorf("SSID = %q, want nil: the SSID lookup is macOS-only", *got.SSID)
	}
}

func TestSnapshotWindows(t *testing.T) {
	withOS(t, "windows")
	withCommands(t, map[string]commandResult{
		"route print -4": {output: `
IPv4 Route Table
===========================================================================
Active Routes:
Network Destination        Netmask          Gateway       Interface  Metric
          0.0.0.0          0.0.0.0      192.168.1.1     192.168.1.23     25
    192.168.1.0    255.255.255.0         On-link      192.168.1.23    281
===========================================================================
`},
		"ipconfig /all": {output: `
Ethernet adapter Ethernet:

   Description . . . . . . . . . . . : Example Gigabit Adapter
   IPv4 Address. . . . . . . . . . . : 192.168.1.23(Preferred)
   DNS Servers . . . . . . . . . . . : 192.168.1.1
                                       1.1.1.1
   NetBIOS over Tcpip. . . . . . . . : Enabled
`},
	})
	withInterfaces(t, []netInterface{
		{Name: "Loopback Pseudo-Interface 1", Flags: upInterface | net.FlagLoopback, Addresses: addrs("127.0.0.1/8")},
		{Name: "Ethernet", Flags: upInterface, Addresses: addrs("192.168.1.23/24")},
	}, nil)

	got := Snapshot()

	if got.OS != "windows" {
		t.Errorf("OS = %q, want %q", got.OS, "windows")
	}
	// The route table only gives an address; the name comes from the
	// interface list.
	assertPointer(t, "ActiveInterface", got.ActiveInterface, "Ethernet")
	assertPointer(t, "DefaultGateway", got.DefaultGateway, "192.168.1.1")
	assertPointer(t, "InterfaceKind", got.InterfaceKind, "other")
	assertSlice(t, "LocalIPv4", got.LocalIPv4, []string{"192.168.1.23"})
	assertSlice(t, "DNSServers", got.DNSServers, []string{"1.1.1.1", "192.168.1.1"})
}

// TestSnapshotDegradation is the important one: a snapshot is decoration on a
// speed test, so no missing tool, failing tool, timeout or empty reply may
// stop it returning a usable result. Unknown fields must come back nil rather
// than guessed, and nothing may panic.
func TestSnapshotDegradation(t *testing.T) {
	tests := []struct {
		name         string
		goos         string
		responses    map[string]commandResult
		interfaces   []netInterface
		interfaceErr error
		wantOS       string
	}{
		{
			name:      "every command missing",
			goos:      "darwin",
			responses: map[string]commandResult{},
			wantOS:    "macOS",
		},
		{
			name: "every command exits non-zero",
			goos: "darwin",
			responses: map[string]commandResult{
				"sw_vers":      {output: "usage: sw_vers", err: errExitStatus1},
				"route":        {output: "route: writing to routing socket: not in table", err: errExitStatus1},
				"scutil":       {err: errExitStatus1},
				"networksetup": {err: errExitStatus1},
			},
			wantOS: "macOS",
		},
		{
			name: "every command times out",
			goos: "darwin",
			responses: map[string]commandResult{
				"sw_vers":      {err: context.DeadlineExceeded},
				"route":        {err: context.DeadlineExceeded},
				"scutil":       {err: context.DeadlineExceeded},
				"networksetup": {err: context.DeadlineExceeded},
			},
			wantOS: "macOS",
		},
		{
			name: "every command succeeds but prints nothing",
			goos: "darwin",
			responses: map[string]commandResult{
				"sw_vers":      {output: ""},
				"route":        {output: ""},
				"scutil":       {output: ""},
				"networksetup": {output: ""},
			},
			wantOS: "macOS",
		},
		{
			name: "commands print only whitespace",
			goos: "darwin",
			responses: map[string]commandResult{
				"sw_vers":      {output: "   \n\t\n"},
				"route":        {output: "\n\n   \n"},
				"scutil":       {output: "\n"},
				"networksetup": {output: "  \n"},
			},
			wantOS: "macOS",
		},
		{
			name:      "linux with no ip command and no resolv.conf",
			goos:      "linux",
			responses: map[string]commandResult{},
			wantOS:    "linux",
		},
		{
			name:      "unsupported platform has no route command at all",
			goos:      "freebsd",
			responses: map[string]commandResult{},
			wantOS:    "freebsd",
		},
		{
			name:         "interface enumeration fails",
			goos:         "linux",
			responses:    map[string]commandResult{},
			interfaceErr: errors.New("route ip+net: netlinkrib: permission denied"),
			wantOS:       "linux",
		},
		{
			name: "default route found but interface list is empty",
			goos: "linux",
			responses: map[string]commandResult{
				"ip route show default": {output: "default via 10.0.0.1 dev eth0\n"},
			},
			interfaces: []netInterface{},
			wantOS:     "linux",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withOS(t, test.goos)
			withCommands(t, test.responses)
			withInterfaces(t, test.interfaces, test.interfaceErr)

			got := Snapshot()

			if got.OS != test.wantOS {
				t.Errorf("OS = %q, want %q", got.OS, test.wantOS)
			}
			// The measurement still has to be able to report an OS and
			// architecture even when every probe failed.
			if got.Architecture == "" {
				t.Error("Architecture is empty; it never depends on a command")
			}
			if len(got.LocalIPv4) != 0 {
				t.Errorf("LocalIPv4 = %#v, want empty", got.LocalIPv4)
			}
			if len(got.LocalIPv6) != 0 {
				t.Errorf("LocalIPv6 = %#v, want empty", got.LocalIPv6)
			}
			if len(got.DNSServers) != 0 {
				t.Errorf("DNSServers = %#v, want empty", got.DNSServers)
			}
			if got.SSID != nil {
				t.Errorf("SSID = %q, want nil", *got.SSID)
			}
			if got.BSSID != nil {
				t.Errorf("BSSID = %q, want nil", *got.BSSID)
			}
			// The one case that legitimately learns a route still must not
			// invent an interface name for an empty interface list.
			if test.name == "default route found but interface list is empty" {
				assertPointer(t, "ActiveInterface", got.ActiveInterface, "eth0")
				assertPointer(t, "DefaultGateway", got.DefaultGateway, "10.0.0.1")
				assertPointer(t, "InterfaceKind", got.InterfaceKind, "ethernet")
				return
			}
			if got.ActiveInterface != nil {
				t.Errorf("ActiveInterface = %q, want nil", *got.ActiveInterface)
			}
			if got.DefaultGateway != nil {
				t.Errorf("DefaultGateway = %q, want nil", *got.DefaultGateway)
			}
			if got.InterfaceKind != nil {
				t.Errorf("InterfaceKind = %q, want nil", *got.InterfaceKind)
			}
		})
	}
}

// TestSnapshotSurvivesHostileOutput guards the same promise against output no
// parser was written for. A snapshot must never be able to abort a run.
func TestSnapshotSurvivesHostileOutput(t *testing.T) {
	outputs := map[string]string{
		"nil bytes":        "",
		"binary":           "\x00\x01\x02\xff\xfe",
		"no newline":       "default via",
		"colon only":       ":",
		"trailing colon":   "interface:",
		"huge single line": strings.Repeat("a", 1<<16),
		"many colons":      strings.Repeat(":", 4096),
		"only separators":  "\n\n\n\n",
		"cut key no value": "gateway",
	}
	for _, goos := range []string{"darwin", "linux", "windows", "openbsd"} {
		for name, output := range outputs {
			t.Run(goos+"/"+name, func(t *testing.T) {
				withOS(t, goos)
				withRunCommand(t, func(context.Context, string, ...string) ([]byte, error) {
					return []byte(output), nil
				})
				withInterfaces(t, nil, nil)
				withResolvConf(t, output)
				// The assertion is that this returns at all.
				if got := Snapshot(); got.Architecture != runtime.GOARCH {
					t.Errorf("Architecture = %q", got.Architecture)
				}
			})
		}
	}
}

func TestOperatingSystem(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		responses map[string]commandResult
		want      string
	}{
		{name: "linux never shells out", goos: "linux", want: "linux"},
		{name: "windows never shells out", goos: "windows", want: "windows"},
		{
			name:      "darwin reports the product version",
			goos:      "darwin",
			responses: map[string]commandResult{"sw_vers": {output: "15.3.1\n"}},
			want:      "macOS 15.3.1",
		},
		{
			name:      "darwin trims surrounding whitespace",
			goos:      "darwin",
			responses: map[string]commandResult{"sw_vers": {output: "  26.0  \r\n"}},
			want:      "macOS 26.0",
		},
		{
			name:      "sw_vers missing falls back to the bare name",
			goos:      "darwin",
			responses: map[string]commandResult{},
			want:      "macOS",
		},
		{
			name:      "sw_vers fails falls back to the bare name",
			goos:      "darwin",
			responses: map[string]commandResult{"sw_vers": {output: "15.3.1", err: errExitStatus1}},
			want:      "macOS",
		},
		{
			name:      "sw_vers prints nothing falls back to the bare name",
			goos:      "darwin",
			responses: map[string]commandResult{"sw_vers": {output: "  \n"}},
			want:      "macOS",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withOS(t, test.goos)
			withCommands(t, test.responses)
			if got := operatingSystem(context.Background()); got != test.want {
				t.Errorf("operatingSystem() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDefaultRoute(t *testing.T) {
	tests := []struct {
		name          string
		goos          string
		responses     map[string]commandResult
		interfaces    []netInterface
		wantInterface string
		wantGateway   string
	}{
		{
			name:          "darwin",
			goos:          "darwin",
			responses:     map[string]commandResult{"route": {output: darwinRouteOutput}},
			wantInterface: "en0",
			wantGateway:   "192.168.1.1",
		},
		{
			name: "darwin over a VPN tunnel with an IPv6 gateway",
			goos: "darwin",
			responses: map[string]commandResult{"route": {output: `   route to: default
    gateway: fe80::1%utun4
  interface: utun4
`}},
			wantInterface: "utun4",
			wantGateway:   "fe80::1%utun4",
		},
		{
			name:      "darwin with no default route",
			goos:      "darwin",
			responses: map[string]commandResult{"route": {output: "   route to: default\nroute: writing to routing socket: not in table\n"}},
		},
		{
			name:      "darwin route command fails",
			goos:      "darwin",
			responses: map[string]commandResult{"route": {err: errNotInstalled}},
		},
		{
			name:          "linux",
			goos:          "linux",
			responses:     map[string]commandResult{"ip": {output: "default via 10.0.0.1 dev eth0 proto dhcp src 10.0.0.42 metric 100\n"}},
			wantInterface: "eth0",
			wantGateway:   "10.0.0.1",
		},
		{
			name:          "linux point-to-point route has a device but no gateway",
			goos:          "linux",
			responses:     map[string]commandResult{"ip": {output: "default dev wg0 scope link\n"}},
			wantInterface: "wg0",
		},
		{
			name:      "linux with no default route prints nothing",
			goos:      "linux",
			responses: map[string]commandResult{"ip": {output: ""}},
		},
		{
			name:      "linux ip command missing",
			goos:      "linux",
			responses: map[string]commandResult{},
		},
		{
			name: "windows resolves the interface name from its address",
			goos: "windows",
			responses: map[string]commandResult{"route": {output: `Active Routes:
          0.0.0.0          0.0.0.0      192.168.1.1     192.168.1.23     25
`}},
			interfaces:    []netInterface{{Name: "Wi-Fi", Flags: upInterface, Addresses: addrs("192.168.1.23/24")}},
			wantInterface: "Wi-Fi",
			wantGateway:   "192.168.1.1",
		},
		{
			name: "windows keeps the gateway when no interface matches",
			goos: "windows",
			responses: map[string]commandResult{"route": {output: `          0.0.0.0          0.0.0.0      192.168.1.1     192.168.1.23     25
`}},
			interfaces:  []netInterface{{Name: "Wi-Fi", Flags: upInterface, Addresses: addrs("10.0.0.5/24")}},
			wantGateway: "192.168.1.1",
		},
		{
			// A VPN client's on-link default route still identifies the active
			// interface; only the gateway is absent.
			name: "windows on-link default route resolves the interface with no gateway",
			goos: "windows",
			responses: map[string]commandResult{"route": {output: `          0.0.0.0          0.0.0.0         On-link        10.8.0.2     25
`}},
			interfaces:    []netInterface{{Name: "OpenVPN TAP", Flags: upInterface, Addresses: addrs("10.8.0.2/24")}},
			wantInterface: "OpenVPN TAP",
		},
		{
			name:      "windows route command fails",
			goos:      "windows",
			responses: map[string]commandResult{"route": {err: errExitStatus1}},
		},
		{
			name: "unsupported platform runs no command",
			goos: "plan9",
			responses: map[string]commandResult{
				"route": {output: darwinRouteOutput},
				"ip":    {output: "default via 10.0.0.1 dev eth0\n"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withOS(t, test.goos)
			withCommands(t, test.responses)
			withInterfaces(t, test.interfaces, nil)
			gotInterface, gotGateway := defaultRoute(context.Background())
			if gotInterface != test.wantInterface || gotGateway != test.wantGateway {
				t.Errorf("defaultRoute() = %q/%q, want %q/%q",
					gotInterface, gotGateway, test.wantInterface, test.wantGateway)
			}
		})
	}
}

func TestDNSServers(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
		responses  map[string]commandResult
		resolvConf *string
		want       []string
	}{
		{
			name:      "darwin reads scutil",
			goos:      "darwin",
			responses: map[string]commandResult{"scutil": {output: darwinDNSOutput}},
			want:      []string{"1.1.1.1", "192.168.1.1"},
		},
		{
			name:       "darwin falls back to resolv.conf when scutil fails",
			goos:       "darwin",
			responses:  map[string]commandResult{"scutil": {err: errExitStatus1}},
			resolvConf: pointerTo("nameserver 9.9.9.9\n"),
			want:       []string{"9.9.9.9"},
		},
		{
			name:      "windows reads ipconfig",
			goos:      "windows",
			responses: map[string]commandResult{"ipconfig": {output: "   DNS Servers . . . . . . . . . . . : 8.8.8.8\n                                       8.8.4.4\n"}},
			want:      []string{"8.8.4.4", "8.8.8.8"},
		},
		{
			// Windows reports its site-local resolver with a zone index.
			// localAddresses already strips %zone, so dropping the address here
			// would leave the two paths disagreeing about the same input.
			name:      "windows keeps a zone-suffixed resolver, without the zone",
			goos:      "windows",
			responses: map[string]commandResult{"ipconfig": {output: "   DNS Servers . . . . . . . . . . . : fec0:0:0:ffff::1%1\n                                       fec0:0:0:ffff::2%1\n"}},
			want:      []string{"fec0:0:0:ffff::1", "fec0:0:0:ffff::2"},
		},
		{
			// The zone-suffixed continuation line must not be mistaken for a
			// new "label: value" pair, which would end the run of servers and
			// silently drop every address after the first.
			name:      "windows zone-suffixed continuation does not end the DNS run",
			goos:      "windows",
			responses: map[string]commandResult{"ipconfig": {output: "   DNS Servers . . . . . . . . . . . : 8.8.8.8\n                                       fec0:0:0:ffff::1%1\n                                       8.8.4.4\n   NetBIOS over Tcpip. . . . . . . . : Enabled\n"}},
			want:      []string{"8.8.4.4", "8.8.8.8", "fec0:0:0:ffff::1"},
		},
		{
			name:       "linux resolv.conf keeps a zone-suffixed nameserver",
			goos:       "linux",
			resolvConf: pointerTo("nameserver fe80::1%eth0\n"),
			want:       []string{"fe80::1"},
		},
		{
			name:       "windows falls back to resolv.conf when ipconfig fails",
			goos:       "windows",
			responses:  map[string]commandResult{"ipconfig": {err: errExitStatus1}},
			resolvConf: pointerTo("nameserver 1.0.0.1\n"),
			want:       []string{"1.0.0.1"},
		},
		{
			name:       "linux reads resolv.conf",
			goos:       "linux",
			resolvConf: pointerTo("nameserver 127.0.0.53\nsearch lan\n"),
			want:       []string{"127.0.0.53"},
		},
		{
			name:       "IPv6-only resolver",
			goos:       "linux",
			resolvConf: pointerTo("nameserver 2606:4700:4700::1111\nnameserver 2606:4700:4700::1001\n"),
			want:       []string{"2606:4700:4700::1001", "2606:4700:4700::1111"},
		},
		{
			name:       "resolv.conf with no nameservers",
			goos:       "linux",
			resolvConf: pointerTo("# no servers here\nsearch lan\n"),
			want:       []string{},
		},
		{
			name: "resolv.conf missing yields nil rather than an error",
			goos: "linux",
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withOS(t, test.goos)
			withCommands(t, test.responses)
			if test.resolvConf != nil {
				withResolvConf(t, *test.resolvConf)
			}
			got := dnsServers(context.Background())
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("dnsServers() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestLocalAddresses(t *testing.T) {
	tests := []struct {
		name            string
		activeInterface string
		interfaces      []netInterface
		err             error
		wantIPv4        []string
		wantIPv6        []string
	}{
		{
			name:            "only the active interface is reported",
			activeInterface: "en0",
			interfaces: []netInterface{
				{Name: "en0", Flags: upInterface, Addresses: addrs("192.168.1.23/24", "2001:db8::23/64")},
				{Name: "en5", Flags: upInterface, Addresses: addrs("10.9.9.9/24")},
			},
			wantIPv4: []string{"192.168.1.23"},
			wantIPv6: []string{"2001:db8::23"},
		},
		{
			name: "with no active interface every up interface counts",
			interfaces: []netInterface{
				{Name: "en0", Flags: upInterface, Addresses: addrs("192.168.1.23/24")},
				{Name: "en5", Flags: upInterface, Addresses: addrs("10.9.9.9/24")},
			},
			wantIPv4: []string{"10.9.9.9", "192.168.1.23"},
		},
		{
			name: "down interfaces are skipped",
			interfaces: []netInterface{
				{Name: "en0", Flags: 0, Addresses: addrs("192.168.1.23/24")},
			},
		},
		{
			name: "loopback is skipped even when up",
			interfaces: []netInterface{
				{Name: "lo0", Flags: upInterface | net.FlagLoopback, Addresses: addrs("127.0.0.1/8", "::1/128")},
			},
		},
		{
			name: "loopback addresses on a non-loopback interface are skipped",
			interfaces: []netInterface{
				{Name: "en0", Flags: upInterface, Addresses: addrs("127.0.0.2/8", "192.168.1.23/24")},
			},
			wantIPv4: []string{"192.168.1.23"},
		},
		{
			name: "IPv6-only host",
			interfaces: []netInterface{
				{Name: "eth0", Flags: upInterface, Addresses: addrs("2001:db8::1/64", "2001:db8::2/64")},
			},
			wantIPv6: []string{"2001:db8::1", "2001:db8::2"},
		},
		{
			name: "zone suffixed link-local addresses lose the zone",
			interfaces: []netInterface{
				{Name: "en0", Flags: upInterface, Addresses: addrs("fe80::1%en0")},
			},
			wantIPv6: []string{"fe80::1"},
		},
		{
			name: "duplicates collapse and results are sorted",
			interfaces: []netInterface{
				{Name: "en0", Flags: upInterface, Addresses: addrs("192.168.1.23/24", "192.168.1.23/24", "10.0.0.5/8")},
			},
			wantIPv4: []string{"10.0.0.5", "192.168.1.23"},
		},
		{
			name: "unparseable addresses are dropped, not fatal",
			interfaces: []netInterface{
				{Name: "en0", Flags: upInterface, Addresses: addrs("not-an-address", "", "192.168.1.23/24")},
			},
			wantIPv4: []string{"192.168.1.23"},
		},
		{
			name:            "active interface that does not exist yields nothing",
			activeInterface: "ppp0",
			interfaces: []netInterface{
				{Name: "en0", Flags: upInterface, Addresses: addrs("192.168.1.23/24")},
			},
		},
		{
			name: "enumeration failure yields nil",
			err:  errors.New("netlinkrib: permission denied"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withInterfaces(t, test.interfaces, test.err)
			gotIPv4, gotIPv6 := localAddresses(test.activeInterface)
			assertSlice(t, "IPv4", gotIPv4, test.wantIPv4)
			assertSlice(t, "IPv6", gotIPv6, test.wantIPv6)
		})
	}
}

func TestInterfaceNameForAddress(t *testing.T) {
	interfaces := []netInterface{
		{Name: "Ethernet", Flags: upInterface, Addresses: addrs("192.168.1.23/24", "bad")},
		{Name: "Wi-Fi", Flags: upInterface, Addresses: addrs("10.0.0.5/8", "2001:db8::7/64")},
	}
	tests := []struct {
		name    string
		address string
		err     error
		want    string
	}{
		{name: "matches an IPv4 address", address: "10.0.0.5", want: "Wi-Fi"},
		{name: "matches an IPv6 address", address: "2001:db8::7", want: "Wi-Fi"},
		{name: "tolerates surrounding whitespace", address: "  192.168.1.23 ", want: "Ethernet"},
		{name: "unknown address yields nothing", address: "172.16.0.1"},
		{name: "empty address short-circuits", address: ""},
		{name: "whitespace-only address short-circuits", address: "   "},
		{name: "enumeration failure yields nothing", address: "10.0.0.5", err: errors.New("denied")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.err != nil {
				withInterfaces(t, nil, test.err)
			} else {
				withInterfaces(t, interfaces, nil)
			}
			if got := interfaceNameForAddress(test.address); got != test.want {
				t.Errorf("interfaceNameForAddress(%q) = %q, want %q", test.address, got, test.want)
			}
		})
	}
}

func TestMacHardwarePort(t *testing.T) {
	tests := []struct {
		name            string
		activeInterface string
		responses       map[string]commandResult
		want            string
	}{
		{
			name:            "finds the port for the active device",
			activeInterface: "en0",
			responses:       map[string]commandResult{"networksetup": {output: darwinHardwarePortsOutput}},
			want:            "Wi-Fi",
		},
		{
			name:            "unknown device yields nothing",
			activeInterface: "utun4",
			responses:       map[string]commandResult{"networksetup": {output: darwinHardwarePortsOutput}},
		},
		{
			name:            "networksetup missing yields nothing",
			activeInterface: "en0",
			responses:       map[string]commandResult{},
		},
		{
			name:            "networksetup fails yields nothing",
			activeInterface: "en0",
			responses:       map[string]commandResult{"networksetup": {output: darwinHardwarePortsOutput, err: errExitStatus1}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withCommands(t, test.responses)
			if got := macHardwarePort(context.Background(), test.activeInterface); got != test.want {
				t.Errorf("macHardwarePort() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMacSSID(t *testing.T) {
	tests := []struct {
		name      string
		responses map[string]commandResult
		want      string
	}{
		{
			name:      "reports the joined network",
			responses: map[string]commandResult{"networksetup": {output: "Current Wi-Fi Network: Example Network\n"}},
			want:      "Example Network",
		},
		{
			name:      "an SSID containing a colon keeps everything after the first one",
			responses: map[string]commandResult{"networksetup": {output: "Current Wi-Fi Network: Floor 2: Guest\n"}},
			want:      "Floor 2: Guest",
		},
		{
			// Older macOS phrases the label differently; the parser only
			// depends on there being a colon.
			name:      "older AirPort wording still parses",
			responses: map[string]commandResult{"networksetup": {output: "Current AirPort Network: Example Network"}},
			want:      "Example Network",
		},
		{
			name:      "not associated yields nothing",
			responses: map[string]commandResult{"networksetup": {output: "You are not associated with an AirPort network.\n"}},
		},
		{
			// Shape invented to cover a localised "not associated" reply that
			// still contains a colon.
			name:      "a line with no colon yields nothing",
			responses: map[string]commandResult{"networksetup": {output: "en5 is not a Wi-Fi interface\n"}},
		},
		{
			name:      "empty output yields nothing",
			responses: map[string]commandResult{"networksetup": {output: ""}},
		},
		{
			name:      "command missing yields nothing",
			responses: map[string]commandResult{},
		},
		{
			name:      "command fails yields nothing",
			responses: map[string]commandResult{"networksetup": {output: "Current Wi-Fi Network: Example Network", err: errExitStatus1}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withCommands(t, test.responses)
			if got := macSSID(context.Background(), "en0"); got != test.want {
				t.Errorf("macSSID() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestSnapshotDarwinInterfaceKinds pins how a macOS hardware port overrides
// the name-based guess, including the case where the port lookup fails and the
// name-based classification has to stand on its own.
func TestSnapshotDarwinInterfaceKinds(t *testing.T) {
	tests := []struct {
		name          string
		device        string
		hardwarePorts string
		wantKind      string
		wantSSID      string
	}{
		{
			name:          "wifi port sets the kind and looks up the SSID",
			device:        "en0",
			hardwarePorts: "Hardware Port: Wi-Fi\nDevice: en0\n",
			wantKind:      "wifi",
			wantSSID:      "Example Network",
		},
		{
			name:          "ethernet port suppresses the SSID lookup",
			device:        "en5",
			hardwarePorts: "Hardware Port: Ethernet\nDevice: en5\n",
			wantKind:      "ethernet",
		},
		{
			name:          "thunderbolt bridge counts as ethernet",
			device:        "bridge0",
			hardwarePorts: "Hardware Port: Thunderbolt Bridge\nDevice: bridge0\n",
			wantKind:      "ethernet",
		},
		{
			// networksetup does not list utun devices, so the port lookup
			// comes back empty and the name-based guess must survive.
			name:          "tunnel device with no hardware port entry",
			device:        "utun4",
			hardwarePorts: darwinHardwarePortsOutput,
			wantKind:      "tunnel",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withOS(t, "darwin")
			withCommands(t, map[string]commandResult{
				"sw_vers":                            {output: "15.3.1\n"},
				"route":                              {output: "  interface: " + test.device + "\n    gateway: 192.168.1.1\n"},
				"scutil":                             {output: darwinDNSOutput},
				"networksetup -listallhardwareports": {output: test.hardwarePorts},
				"networksetup -getairportnetwork " + test.device: {output: "Current Wi-Fi Network: Example Network\n"},
			})
			withInterfaces(t, nil, nil)

			got := Snapshot()

			assertPointer(t, "InterfaceKind", got.InterfaceKind, test.wantKind)
			if test.wantSSID == "" {
				if got.SSID != nil {
					t.Errorf("SSID = %q, want nil", *got.SSID)
				}
				return
			}
			assertPointer(t, "SSID", got.SSID, test.wantSSID)
		})
	}
}

func assertPointer(t *testing.T, field string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %q", field, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %q, want %q", field, *got, want)
	}
}

func assertSlice(t *testing.T, field string, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", field, got, want)
	}
}

func pointerTo(value string) *string { return &value }
