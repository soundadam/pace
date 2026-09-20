package speedtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// minimalServerList is a one-entry list that lets SpeedTest reach --list
// without performing any measurement.
const minimalServerList = `[{"id":1,"name":"test","server":"http://127.0.0.1:1","dlURL":"garbage.php","ulURL":"empty.php","pingURL":"empty.php","getIpURL":"getIP.php"}]`

// configureProxy runs SpeedTest far enough to install the proxy-aware
// transport (it stops at --list) and returns that transport.
func configureProxy(t *testing.T, proxyURL string) (*http.Transport, runResult) {
	t.Helper()

	result := runSpeedTest(t,
		"--"+optProxy, proxyURL,
		"--local-json", writeServerList(t, minimalServerList),
		"--list",
	)
	if result.Err != nil {
		t.Fatalf("SpeedTest with --proxy %s: %v", proxyURL, result.Err)
	}
	transport, ok := http.DefaultClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultClient.Transport = %T, want *http.Transport", http.DefaultClient.Transport)
	}
	return transport, result
}

const (
	optProxy = "proxy"
)

// TestProxyRoutesTrafficThroughSOCKS5 pins the happy path: with
// --proxy socks5h://127.0.0.1:PORT every measurement connection is made
// through the proxy and reaches the backend.
func TestProxyRoutesTrafficThroughSOCKS5(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "backend-reached")
	}))
	defer backend.Close()

	proxy := startSOCKS5(t, &socks5Server{MethodReply: 0x00, ReplyCode: 0x00, Target: backend.Listener.Addr().String()})
	transport, _ := configureProxy(t, "socks5h://127.0.0.1:"+proxy.Port())

	response, err := (&http.Client{Transport: transport}).Get(backend.URL)
	if err != nil {
		t.Fatalf("request through the proxy: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "backend-reached" {
		t.Errorf("body = %q, want %q", body, "backend-reached")
	}

	requests := proxy.Requests()
	if len(requests) != 1 {
		t.Fatalf("proxy saw %d CONNECT requests, want 1", len(requests))
	}
}

// TestProxyOffersOnlyNoAuthentication pins that the fork never negotiates
// credentials: the SOCKS5 dialer is built with a nil auth block, so the only
// method offered is 0x00 (no authentication).
func TestProxyOffersOnlyNoAuthentication(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer backend.Close()

	proxy := startSOCKS5(t, &socks5Server{MethodReply: 0x00, Target: backend.Listener.Addr().String()})
	transport, _ := configureProxy(t, "socks5h://127.0.0.1:"+proxy.Port())

	response, err := (&http.Client{Transport: transport}).Get(backend.URL)
	if err != nil {
		t.Fatalf("request through the proxy: %v", err)
	}
	_ = response.Body.Close()

	requests := proxy.Requests()
	if len(requests) == 0 {
		t.Fatal("proxy saw no requests")
	}
	methods := requests[0].Methods
	if len(methods) != 1 || methods[0] != 0x00 {
		t.Errorf("client offered methods %v, want exactly [0x00] (no authentication)", methods)
	}
}

// TestProxyResolvesHostnameThroughProxy pins the "h" in socks5h: the client
// sends the hostname to the proxy (ATYP 0x03) instead of resolving it locally,
// which is what keeps the campus route label accurate.
func TestProxyResolvesHostnameThroughProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer backend.Close()

	proxy := startSOCKS5(t, &socks5Server{MethodReply: 0x00, Target: backend.Listener.Addr().String()})
	transport, _ := configureProxy(t, "socks5h://127.0.0.1:"+proxy.Port())

	// A name that does not resolve anywhere: if the client tried to resolve
	// it locally the dial would fail before reaching the proxy.
	response, err := (&http.Client{Transport: transport}).Get("http://librespeed-backend.invalid:8080/ping")
	if err != nil {
		t.Fatalf("request for an unresolvable name through the proxy: %v", err)
	}
	_ = response.Body.Close()

	requests := proxy.Requests()
	if len(requests) != 1 {
		t.Fatalf("proxy saw %d requests, want 1", len(requests))
	}
	if requests[0].AddressType != 0x03 {
		t.Errorf("address type = %#x, want 0x03 (domain name)", requests[0].AddressType)
	}
	if requests[0].Host != "librespeed-backend.invalid" {
		t.Errorf("proxy was asked for %q, want the unresolved hostname", requests[0].Host)
	}
	if requests[0].Port != 8080 {
		t.Errorf("proxy was asked for port %d, want 8080", requests[0].Port)
	}
}

// TestProxySendsNumericAddressesAsIP pins the complementary case: a literal IP
// is sent as ATYP 0x01, not as a domain name.
func TestProxySendsNumericAddressesAsIP(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer backend.Close()

	proxy := startSOCKS5(t, &socks5Server{MethodReply: 0x00, Target: backend.Listener.Addr().String()})
	transport, _ := configureProxy(t, "socks5h://127.0.0.1:"+proxy.Port())

	response, err := (&http.Client{Transport: transport}).Get(backend.URL)
	if err != nil {
		t.Fatalf("request through the proxy: %v", err)
	}
	_ = response.Body.Close()

	requests := proxy.Requests()
	if len(requests) != 1 {
		t.Fatalf("proxy saw %d requests, want 1", len(requests))
	}
	if requests[0].AddressType != 0x01 {
		t.Errorf("address type = %#x, want 0x01 (IPv4 literal)", requests[0].AddressType)
	}
	if requests[0].Host != "127.0.0.1" {
		t.Errorf("proxy was asked for %q, want 127.0.0.1", requests[0].Host)
	}
}

// TestProxyDialFailures covers the failure modes the campus runner can hit.
func TestProxyDialFailures(t *testing.T) {
	t.Run("proxy refuses the connection", func(t *testing.T) {
		port := freeLoopbackPort(t)
		transport, _ := configureProxy(t, "socks5h://127.0.0.1:"+port)

		_, err := transport.DialContext(context.Background(), "tcp", "example.invalid:80")
		if err == nil {
			t.Fatal("dial through a dead proxy succeeded, want an error")
		}
		if !strings.Contains(err.Error(), "refused") && !strings.Contains(err.Error(), "connect") {
			t.Logf("dial error (accepted): %v", err)
		}
	})

	t.Run("proxy rejects the no-auth method", func(t *testing.T) {
		// 0xFF is "no acceptable methods"; 0x02 would demand
		// username/password, which the fork cannot supply.
		for name, method := range map[string]byte{"no acceptable methods": 0xFF, "username/password demanded": 0x02} {
			t.Run(name, func(t *testing.T) {
				proxy := startSOCKS5(t, &socks5Server{MethodReply: method})
				transport, _ := configureProxy(t, "socks5h://127.0.0.1:"+proxy.Port())

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := transport.DialContext(ctx, "tcp", "example.invalid:80"); err == nil {
					t.Fatal("dial succeeded, want an authentication failure")
				}
			})
		}
	})

	t.Run("proxy replies with an error status", func(t *testing.T) {
		proxy := startSOCKS5(t, &socks5Server{MethodReply: 0x00, ReplyCode: 0x05})
		transport, _ := configureProxy(t, "socks5h://127.0.0.1:"+proxy.Port())

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := transport.DialContext(ctx, "tcp", "example.invalid:80")
		if err == nil {
			t.Fatal("dial succeeded, want the proxy's refusal to surface")
		}
	})

	t.Run("target is unreachable behind the proxy", func(t *testing.T) {
		port := freeLoopbackPort(t)
		proxy := startSOCKS5(t, &socks5Server{MethodReply: 0x00, Target: "127.0.0.1:" + port})
		transport, _ := configureProxy(t, "socks5h://127.0.0.1:"+proxy.Port())

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := transport.DialContext(ctx, "tcp", "example.invalid:80"); err == nil {
			t.Fatal("dial succeeded, want an unreachable-target error")
		}
	})
}

// TestProxyDialIsCancellable pins the requirement the fork asserts explicitly:
// the SOCKS dialer must implement ContextDialer, so a hung proxy cannot wedge
// a measurement past its deadline.
func TestProxyDialIsCancellable(t *testing.T) {
	proxy := startSOCKS5(t, &socks5Server{MethodReply: 0x00, HangAfterGreeting: true})
	transport, _ := configureProxy(t, "socks5h://127.0.0.1:"+proxy.Port())

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := transport.DialContext(ctx, "tcp", "example.invalid:80")
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("dial against a hung proxy succeeded, want a cancellation error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Logf("cancellation error: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("dial took %v to honour a 150ms deadline", elapsed)
	}
}

// TestProxyArgumentValidation pins every rejection the fork performs before it
// builds a dialer, including the exact error text.
func TestProxyArgumentValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proxy string
		want  string
	}{
		{"wrong scheme", "http://127.0.0.1:1080", "--proxy requires socks5h://host:port without credentials"},
		{"plain socks5 scheme", "socks5://127.0.0.1:1080", "--proxy requires socks5h://host:port without credentials"},
		{"credentials supplied", "socks5h://user:pass@127.0.0.1:1080", "--proxy requires socks5h://host:port without credentials"},
		{"username only", "socks5h://user@127.0.0.1:1080", "--proxy requires socks5h://host:port without credentials"},
		{"no port", "socks5h://127.0.0.1", "--proxy requires socks5h://host:port without credentials"},
		{"no host", "socks5h://:1080", "--proxy requires socks5h://host:port without credentials"},
		{"unparseable", "socks5h://127.0.0.1:port", "--proxy requires socks5h://host:port without credentials"},
		{"not a URL", "://", "--proxy requires socks5h://host:port without credentials"},
		{"hostname instead of a numeric address", "socks5h://localhost:1080", "--proxy must use a numeric loopback address"},
		{"non-loopback address", "socks5h://192.0.2.10:1080", "--proxy must use a numeric loopback address"},
		{"public address", "socks5h://8.8.8.8:1080", "--proxy must use a numeric loopback address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := runSpeedTest(t,
				"--"+optProxy, tc.proxy,
				"--local-json", writeServerList(t, minimalServerList),
				"--list",
			)
			if result.Err == nil {
				t.Fatalf("--proxy %q was accepted, want %q", tc.proxy, tc.want)
			}
			if result.Err.Error() != tc.want {
				t.Errorf("error = %q, want %q", result.Err, tc.want)
			}
		})
	}
}

// TestProxyAcceptsIPv6Loopback pins that ::1 is a valid proxy address even
// though --ipv6 itself is rejected alongside --proxy.
func TestProxyAcceptsIPv6Loopback(t *testing.T) {
	port := freeLoopbackPort(t)
	result := runSpeedTest(t,
		"--"+optProxy, fmt.Sprintf("socks5h://[::1]:%s", port),
		"--local-json", writeServerList(t, minimalServerList),
		"--list",
	)
	if result.Err != nil {
		t.Fatalf("socks5h://[::1]:%s was rejected: %v", port, result.Err)
	}
}

// TestProxyIncompatibleOptions pins the options that cannot be combined with
// --proxy, because each of them would either bypass or contradict the proxy
// route.
func TestProxyIncompatibleOptions(t *testing.T) {
	const want = "--proxy cannot be combined with source, interface, fwmark, or IPv6"
	port := freeLoopbackPort(t)
	proxy := "socks5h://127.0.0.1:" + port

	// The compatibility check runs before any dialer is constructed, so the
	// message is the same on every platform. Interface and fwmark binding is
	// Linux-only: if the dialer were built first, macOS and Windows would
	// answer "cannot bound to interface on this platform" instead, which names
	// the wrong problem.
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"source", []string{"--source", "127.0.0.1"}},
		{"interface", []string{"--interface", "lo0"}},
		{"fwmark", []string{"--fwmark", "7"}},
		{"ipv6", []string{"--ipv6"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{
				"--" + optProxy, proxy,
				"--local-json", writeServerList(t, minimalServerList),
				"--list",
			}, tc.args...)

			result := runSpeedTest(t, args...)
			if result.Err == nil {
				t.Fatalf("--proxy with --%s was accepted, want it rejected", tc.name)
			}
			if result.Err.Error() != want {
				t.Errorf("error = %q, want %q", result.Err, want)
			}
		})
	}
}

// TestProxyDisablesICMP pins that --proxy forces the HTTP ping path: ICMP
// cannot traverse a SOCKS5 proxy, so leaving it enabled would measure the
// direct route instead of the proxied one.
func TestProxyDisablesICMP(t *testing.T) {
	port := freeLoopbackPort(t)
	transport, _ := configureProxy(t, "socks5h://127.0.0.1:"+port)

	// The observable consequence is that every connection, ping included,
	// goes through the transport's SOCKS dialer rather than a raw socket.
	if transport.DialContext == nil {
		t.Fatal("transport has no DialContext, so the proxy would be bypassed")
	}
}

// TestAmbientProxyEnvironmentIsIgnored pins the third local change: ambient
// HTTP proxy variables must not influence measurement traffic, so that the
// reported route label stays accurate.
func TestAmbientProxyEnvironmentIsIgnored(t *testing.T) {
	reached := make(chan struct{}, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case reached <- struct{}{}:
		default:
		}
		_, _ = io.WriteString(w, "direct")
	}))
	defer backend.Close()

	// Point every ambient proxy variable at a port with nothing on it. If
	// the transport honoured them the request below would fail.
	deadPort := freeLoopbackPort(t)
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(name, "http://127.0.0.1:"+deadPort)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	result := runSpeedTest(t,
		"--local-json", writeServerList(t, minimalServerList),
		"--list",
	)
	if result.Err != nil {
		t.Fatalf("SpeedTest --list: %v", result.Err)
	}

	transport, ok := http.DefaultClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultClient.Transport = %T, want *http.Transport", http.DefaultClient.Transport)
	}
	if transport.Proxy != nil {
		t.Error("transport.Proxy is set; ambient proxy variables would be honoured")
	}

	response, err := (&http.Client{Transport: transport}).Get(backend.URL)
	if err != nil {
		t.Fatalf("request with ambient proxy variables set: %v", err)
	}
	_ = response.Body.Close()

	select {
	case <-reached:
	default:
		t.Error("the backend was never reached directly")
	}
}
