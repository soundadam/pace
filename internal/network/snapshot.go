package network

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/soundadam/soundprobe/internal/model"
)

const snapshotTimeout = 2 * time.Second

type commandRunner func(context.Context, string, ...string) ([]byte, error)

var runCommand commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// currentOS and resolvConfPath name the platform facts the snapshot branches
// on. They are variables rather than constants so tests can drive every
// platform path from one host.
var (
	currentOS      = runtime.GOOS
	resolvConfPath = "/etc/resolv.conf"
)

// netInterface is one network interface with its addresses already resolved.
// Enumeration goes through systemInterfaces so address discovery can be
// exercised without depending on the host's real interface list.
type netInterface struct {
	Name      string
	Flags     net.Flags
	Addresses []net.Addr
}

var systemInterfaces = func() ([]netInterface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	result := make([]netInterface, 0, len(interfaces))
	for _, networkInterface := range interfaces {
		addresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		result = append(result, netInterface{
			Name:      networkInterface.Name,
			Flags:     networkInterface.Flags,
			Addresses: addresses,
		})
	}
	return result, nil
}

func Snapshot() model.NetworkContext {
	ctx, cancel := context.WithTimeout(context.Background(), snapshotTimeout)
	defer cancel()

	result := model.NetworkContext{
		OS:           operatingSystem(ctx),
		Architecture: runtime.GOARCH,
	}

	activeInterface, gateway := defaultRoute(ctx)
	if activeInterface != "" {
		result.ActiveInterface = model.Pointer(activeInterface)
	}
	if gateway != "" {
		result.DefaultGateway = model.Pointer(gateway)
	}

	result.LocalIPv4, result.LocalIPv6 = localAddresses(activeInterface)
	result.DNSServers = dnsServers(ctx)

	kind := classifyInterface(activeInterface)
	if currentOS == "darwin" && activeInterface != "" {
		if hardwarePort := macHardwarePort(ctx, activeInterface); hardwarePort != "" {
			kind = classifyHardwarePort(hardwarePort)
		}
		if kind == "wifi" {
			if ssid := macSSID(ctx, activeInterface); ssid != "" {
				result.SSID = model.Pointer(ssid)
			}
		}
	}
	if kind != "" {
		result.InterfaceKind = model.Pointer(kind)
	}
	return result
}

func operatingSystem(ctx context.Context) string {
	if currentOS != "darwin" {
		return currentOS
	}
	output, err := runCommand(ctx, "sw_vers", "-productVersion")
	if err != nil {
		return "macOS"
	}
	version := strings.TrimSpace(string(output))
	if version == "" {
		return "macOS"
	}
	return "macOS " + version
}

func defaultRoute(ctx context.Context) (string, string) {
	switch currentOS {
	case "darwin":
		output, err := runCommand(ctx, "route", "-n", "get", "default")
		if err != nil {
			return "", ""
		}
		return parseDarwinRoute(string(output))
	case "linux":
		output, err := runCommand(ctx, "ip", "route", "show", "default")
		if err != nil {
			return "", ""
		}
		return parseLinuxRoute(string(output))
	case "windows":
		output, err := runCommand(ctx, "route", "print", "-4")
		if err != nil {
			return "", ""
		}
		interfaceAddress, gateway := parseWindowsRoute(string(output))
		return interfaceNameForAddress(interfaceAddress), gateway
	default:
		return "", ""
	}
}

func parseDarwinRoute(output string) (string, string) {
	var activeInterface, gateway string
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "interface":
			activeInterface = strings.TrimSpace(value)
		case "gateway":
			gateway = strings.TrimSpace(value)
		}
	}
	return activeInterface, gateway
}

func parseLinuxRoute(output string) (string, string) {
	fields := strings.Fields(output)
	var activeInterface, gateway string
	for index, field := range fields {
		if index+1 >= len(fields) {
			break
		}
		switch field {
		case "dev":
			activeInterface = fields[index+1]
		case "via":
			gateway = fields[index+1]
		}
	}
	return activeInterface, gateway
}

func parseWindowsRoute(output string) (string, string) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "0.0.0.0" || fields[1] != "0.0.0.0" {
			continue
		}
		// Some VPN clients publish an on-link default route, so the gateway
		// column reads "On-link" rather than an address. defaultGateway holds an
		// IP on every other platform, so a non-address is reported as absent
		// rather than leaked through as a literal. The interface address on the
		// same row is unaffected and still drives the active-interface lookup.
		gateway := ""
		if ip := parseIPWithZone(fields[2]); ip != nil {
			gateway = ip.String()
		}
		return fields[3], gateway
	}
	return "", ""
}

func interfaceNameForAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	interfaces, err := systemInterfaces()
	if err != nil {
		return ""
	}
	for _, networkInterface := range interfaces {
		for _, candidate := range networkInterface.Addresses {
			ip, _, err := net.ParseCIDR(candidate.String())
			if err == nil && ip.String() == address {
				return networkInterface.Name
			}
		}
	}
	return ""
}

func localAddresses(activeInterface string) ([]string, []string) {
	interfaces, err := systemInterfaces()
	if err != nil {
		return nil, nil
	}
	var ipv4, ipv6 []string
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if activeInterface != "" && networkInterface.Name != activeInterface {
			continue
		}
		for _, address := range networkInterface.Addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil {
				ip = parseIPWithZone(address.String())
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ip.To4() != nil {
				ipv4 = append(ipv4, ip.String())
			} else {
				ipv6 = append(ipv6, ip.String())
			}
		}
	}
	return uniqueSorted(ipv4), uniqueSorted(ipv6)
}

func dnsServers(ctx context.Context) []string {
	if currentOS == "darwin" {
		output, err := runCommand(ctx, "scutil", "--dns")
		if err == nil {
			return parseDarwinDNS(string(output))
		}
	}
	if currentOS == "windows" {
		output, err := runCommand(ctx, "ipconfig", "/all")
		if err == nil {
			return parseWindowsDNS(string(output))
		}
	}
	data, err := os.ReadFile(resolvConfPath)
	if err != nil {
		return nil
	}
	return parseResolvConf(string(data))
}

func parseDarwinDNS(output string) []string {
	var servers []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "nameserver[") {
			continue
		}
		_, value, found := strings.Cut(line, ":")
		if found {
			servers = appendValidIP(servers, value)
		}
	}
	return uniqueSorted(servers)
}

func parseResolvConf(output string) []string {
	var servers []string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "nameserver" {
			servers = appendValidIP(servers, fields[1])
		}
	}
	return uniqueSorted(servers)
}

func parseWindowsDNS(output string) []string {
	var servers []string
	inDNS := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		// Continuation lines carry nothing but an address. They must be
		// recognised before the "label: value" split, because an IPv6 server
		// contains colons and would otherwise be mistaken for a new label. The
		// zone suffix is tolerated here too, or a "fec0:0:0:ffff::1%1"
		// continuation line would be read as a label and end the run.
		if inDNS && parseIPWithZone(trimmed) != nil {
			servers = appendValidIP(servers, trimmed)
			continue
		}
		if _, value, found := strings.Cut(line, ":"); found {
			label := strings.ToLower(strings.TrimSpace(strings.SplitN(line, ":", 2)[0]))
			inDNS = strings.Contains(label, "dns servers") || strings.Contains(label, "dns server")
			if inDNS {
				servers = appendValidIP(servers, value)
			}
			continue
		}
		// Any other non-empty line ends the run of DNS servers.
		if inDNS && trimmed != "" {
			inDNS = false
		}
	}
	return uniqueSorted(servers)
}

// parseIPWithZone parses an address that may carry a %zone suffix, as Windows
// reports for link-local and site-local resolvers ("fec0:0:0:ffff::1%1") and as
// the Go resolver reports for link-local interface addresses. The zone is
// dropped from the result. Every address-shaped value in this package goes
// through here so that no two paths disagree about what counts as an address.
func parseIPWithZone(raw string) net.IP {
	value, _, _ := strings.Cut(strings.TrimSpace(raw), "%")
	return net.ParseIP(value)
}

func appendValidIP(values []string, raw string) []string {
	if ip := parseIPWithZone(raw); ip != nil {
		return append(values, ip.String())
	}
	return values
}

func macHardwarePort(ctx context.Context, activeInterface string) string {
	output, err := runCommand(ctx, "networksetup", "-listallhardwareports")
	if err != nil {
		return ""
	}
	return parseMacHardwarePort(string(output), activeInterface)
}

func parseMacHardwarePort(output, activeInterface string) string {
	var hardwarePort string
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Hardware Port":
			hardwarePort = strings.TrimSpace(value)
		case "Device":
			if strings.TrimSpace(value) == activeInterface {
				return hardwarePort
			}
		}
	}
	return ""
}

func macSSID(ctx context.Context, activeInterface string) string {
	output, err := runCommand(ctx, "networksetup", "-getairportnetwork", activeInterface)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(output))
	if strings.Contains(strings.ToLower(line), "not associated") {
		return ""
	}
	_, ssid, found := strings.Cut(line, ":")
	if !found {
		return ""
	}
	return strings.TrimSpace(ssid)
}

func classifyHardwarePort(port string) string {
	lower := strings.ToLower(port)
	switch {
	case strings.Contains(lower, "wi-fi"), strings.Contains(lower, "wifi"), strings.Contains(lower, "airport"):
		return "wifi"
	case strings.Contains(lower, "ethernet"), strings.Contains(lower, "thunderbolt"):
		return "ethernet"
	case strings.Contains(lower, "vpn"), strings.Contains(lower, "tunnel"):
		return "tunnel"
	default:
		return "other"
	}
}

func classifyInterface(name string) string {
	switch {
	case name == "":
		return ""
	case strings.HasPrefix(name, "utun"), strings.HasPrefix(name, "tun"), strings.HasPrefix(name, "tap"), strings.HasPrefix(name, "wg"):
		return "tunnel"
	case strings.HasPrefix(name, "eth"), strings.HasPrefix(name, "eno"), strings.HasPrefix(name, "enp"):
		return "ethernet"
	case strings.HasPrefix(name, "wl"):
		return "wifi"
	default:
		return "other"
	}
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
