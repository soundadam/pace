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

	"github.com/soundadam/pace/internal/model"
)

const snapshotTimeout = 2 * time.Second

type commandRunner func(context.Context, string, ...string) ([]byte, error)

var runCommand commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
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
	if runtime.GOOS == "darwin" && activeInterface != "" {
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
	if runtime.GOOS != "darwin" {
		return runtime.GOOS
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
	switch runtime.GOOS {
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
		return fields[3], fields[2]
	}
	return "", ""
}

func interfaceNameForAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, networkInterface := range interfaces {
		addresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		for _, candidate := range addresses {
			ip, _, err := net.ParseCIDR(candidate.String())
			if err == nil && ip.String() == address {
				return networkInterface.Name
			}
		}
	}
	return ""
}

func localAddresses(activeInterface string) ([]string, []string) {
	interfaces, err := net.Interfaces()
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
		addresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil {
				ip = net.ParseIP(strings.Split(address.String(), "%")[0])
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
	if runtime.GOOS == "darwin" {
		output, err := runCommand(ctx, "scutil", "--dns")
		if err == nil {
			return parseDarwinDNS(string(output))
		}
	}
	if runtime.GOOS == "windows" {
		output, err := runCommand(ctx, "ipconfig", "/all")
		if err == nil {
			return parseWindowsDNS(string(output))
		}
	}
	data, err := os.ReadFile("/etc/resolv.conf")
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
		if _, value, found := strings.Cut(line, ":"); found {
			label := strings.ToLower(strings.TrimSpace(strings.SplitN(line, ":", 2)[0]))
			inDNS = strings.Contains(label, "dns servers") || strings.Contains(label, "dns server")
			if inDNS {
				servers = appendValidIP(servers, value)
			}
			continue
		}
		if inDNS {
			if ip := net.ParseIP(trimmed); ip != nil {
				servers = appendValidIP(servers, trimmed)
			} else if trimmed != "" {
				inDNS = false
			}
		}
	}
	return uniqueSorted(servers)
}

func appendValidIP(values []string, raw string) []string {
	value := strings.TrimSpace(raw)
	if ip := net.ParseIP(value); ip != nil {
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
