package claudecode

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"synapseos/internal/testenv"
	"time"
)

// Network attacks on the isolated sandbox (Network, NetIsolated). Each fake
// CLI below acts as a hostile agent: it tries to reach the host and the LAN,
// to accept connections from the host, and to undo the namespace's rules.
// The host side checks the real state: what its own listeners accepted.

var (
	isoOnce sync.Once
	isoNet  Network
	isoErr  error
)

// isolatedNet is the production network, resolved and probed once.
func isolatedNet() (Network, error) {
	isoOnce.Do(func() {
		isoNet, isoErr = ResolveNetwork(NetIsolated)
		if isoErr == nil {
			isoErr = isoNet.Probe()
		}
	})
	return isoNet, isoErr
}

// needIsolated is the isolated network, or a skip saying why it cannot be
// built here.
func needIsolated(t *testing.T) Network {
	t.Helper()
	n, err := isolatedNet()
	if err != nil {
		testenv.Unavailable(t, "isolated network unavailable: %v", err)
	}
	return n
}

// defaultNet is what agentprobe uses by default, the isolated network. A
// host where it cannot be built skips the test (fails it under
// testenv.RequireEnv) rather than quietly running it on the host's network,
// which a real run never does.
func defaultNet(t *testing.T) Network {
	t.Helper()
	return needIsolated(t)
}

// isolatedRunner is hostileRunner on the isolated network, never the host's.
func isolatedRunner(t *testing.T, script string) Runner {
	t.Helper()
	n := needIsolated(t)
	r := hostileRunner(t, script)
	r.Sandbox.Net = n
	return r
}

// counter is a host listener that counts what reaches it.
type counter struct {
	port int
	hits atomic.Int32
}

func tcpCounter(t *testing.T, addr string) *counter {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil
	}
	t.Cleanup(func() { ln.Close() })
	c := &counter{port: ln.Addr().(*net.TCPAddr).Port}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c.hits.Add(1)
			conn.Write([]byte("host-service\n"))
			conn.Close()
		}
	}()
	return c
}

func udpCounter(t *testing.T, addr string) *counter {
	t.Helper()
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil
	}
	t.Cleanup(func() { pc.Close() })
	c := &counter{port: pc.LocalAddr().(*net.UDPAddr).Port}
	go func() {
		buf := make([]byte, 64)
		for {
			if _, _, err := pc.ReadFrom(buf); err != nil {
				return
			}
			c.hits.Add(1)
		}
	}()
	return c
}

// unixCounter listens on an abstract unix socket, which belongs to the
// network namespace: with --share-net the agent reached the host's (X11 and
// D-Bus listen there).
func unixCounter(t *testing.T, name string) *counter {
	t.Helper()
	ln, err := net.Listen("unix", "@"+name)
	if err != nil {
		return nil
	}
	t.Cleanup(func() { ln.Close() })
	c := &counter{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c.hits.Add(1)
			conn.Close()
		}
	}()
	return c
}

// hostAddrs are the host's own non-loopback addresses an agent could dial
// without a zone: every interface's, link-local IPv6 excluded.
func hostAddrs(t *testing.T) []string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(ipn.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		if ip.IsLoopback() || ip.Is6() && ip.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}

// defaultGateways are the host's IPv4 default routers, from /proc/net/route.
func defaultGateways() []string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 3 || fs[1] != "00000000" {
			continue
		}
		b, err := hex.DecodeString(fs[2])
		if err != nil || len(b) != 4 {
			continue
		}
		out = append(out, netip.AddrFrom4([4]byte{b[3], b[2], b[1], b[0]}).String())
	}
	return out
}

// netProbe is one connection the agent tries.
type netProbe struct {
	Kind string `json:"kind"` // tcp, udp, unix
	Addr string `json:"addr"`
	Port int    `json:"port"`
}

// probeScript is a fake CLI that tries every probe with a 3 s timeout and
// prints, per probe, "PROBE kind addr port ok=0|1 err=E ms=N" on stderr.
// For UDP, ok only says the datagram was sent; whether it arrived is the
// host's counter.
func probeScript(pre string, ps []netProbe) string {
	js, _ := json.Marshal(ps)
	return streamInit + pre + `
python3 - >&2 <<'EOF'
import errno, json, socket, sys, time
for p in json.loads(` + strconv.Quote(string(js)) + `):
    kind, addr, port = p["kind"], p["addr"], p["port"]
    if kind == "unix":
        fam, target = socket.AF_UNIX, "\0" + addr
    else:
        fam = socket.AF_INET6 if ":" in addr else socket.AF_INET
        target = (addr, port)
    s = socket.socket(fam, socket.SOCK_DGRAM if kind == "udp" else socket.SOCK_STREAM)
    s.settimeout(3)
    t, ok, err = time.monotonic(), 0, "-"
    try:
        if kind == "udp":
            s.sendto(b"agent", target)
        else:
            s.connect(target)
        ok = 1
    except OSError as e:
        err = errno.errorcode.get(e.errno, str(e)) if e.errno else type(e).__name__
    finally:
        s.close()
    print("PROBE %s %s %d ok=%d err=%s ms=%d" % (kind, addr.replace("\0", ""), port, ok, err, (time.monotonic() - t) * 1000))
    sys.stdout.flush()
EOF
` + streamResult
}

type probeResult struct {
	ok  bool
	err string
	ms  int
}

// probeResults parses probeScript's lines, keyed "kind addr port".
func probeResults(t *testing.T, seen string) map[string]probeResult {
	t.Helper()
	out := map[string]probeResult{}
	for _, line := range strings.Split(seen, "\n") {
		f := strings.Fields(line)
		if len(f) != 7 || f[0] != "PROBE" {
			continue
		}
		ms, _ := strconv.Atoi(strings.TrimPrefix(f[6], "ms="))
		out[f[1]+" "+f[2]+" "+f[3]] = probeResult{ok: f[4] == "ok=1", err: strings.TrimPrefix(f[5], "err="), ms: ms}
	}
	return out
}

func key(p netProbe) string { return fmt.Sprintf("%s %s %d", p.Kind, p.Addr, p.Port) }

// pyList is ss as a Python list literal (JSON's syntax is Python's here).
func pyList(ss []string) string {
	b, _ := json.Marshal(ss)
	return string(b)
}

// The agent tries every way to the host: its loopback over IPv4 and IPv6
// (a service bound to 127.0.0.1 only, as Ollama is by default), every
// address of every interface (docker0, a VPN, the LAN address), the IPv4
// address mapped into IPv6, UDP to the same, and an abstract unix socket.
// None of the host's listeners sees anything.
//
// Formerly TestKnownGapHostLoopbackReachable, which asserted that the agent
// reached the host's loopback through --share-net (open-problems row 47).
func TestHostileNetworkHostUnreachable(t *testing.T) {
	needIsolated(t)
	var ps []netProbe
	var counters []*counter
	add := func(c *counter, p netProbe) {
		if c != nil {
			counters = append(counters, c)
			ps = append(ps, p)
		}
	}
	lo4 := tcpCounter(t, "127.0.0.1:0")
	if lo4 == nil {
		t.Skip("cannot listen on 127.0.0.1")
	}
	add(lo4, netProbe{"tcp", "127.0.0.1", lo4.port})
	add(lo4, netProbe{"tcp", "::ffff:127.0.0.1", lo4.port})
	if lo6 := tcpCounter(t, "[::1]:0"); lo6 != nil {
		add(lo6, netProbe{"tcp", "::1", lo6.port})
	}
	if u := udpCounter(t, "127.0.0.1:0"); u != nil {
		add(u, netProbe{"udp", "127.0.0.1", u.port})
	}
	name := "agentprobe-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	add(unixCounter(t, name), netProbe{"unix", name, 0})
	// One wildcard listener per protocol answers on every host address.
	wild, wildUDP := tcpCounter(t, ":0"), udpCounter(t, ":0")
	if wild == nil || wildUDP == nil {
		t.Fatal("cannot listen on the host's addresses")
	}
	addrs := hostAddrs(t)
	if len(addrs) == 0 {
		t.Log("the host has no non-loopback address; only its loopback is probed")
	}
	for _, a := range addrs {
		add(wild, netProbe{"tcp", a, wild.port})
		add(wildUDP, netProbe{"udp", a, wildUDP.port})
		if ip := netip.MustParseAddr(a); ip.Is4() {
			add(wild, netProbe{"tcp", "::ffff:" + a, wild.port})
		}
	}
	r := isolatedRunner(t, probeScript("", ps))
	_, err, seen := runHostile(t, r)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // let a datagram in flight land before counting
	got := probeResults(t, seen)
	for _, p := range ps {
		res, ok := got[key(p)]
		switch {
		case !ok:
			t.Errorf("%s: the probe did not run: %s", key(p), seen)
		case res.ok && p.Kind != "udp":
			t.Errorf("%s: the agent connected to the host", key(p))
		}
	}
	for i, c := range counters {
		if n := c.hits.Load(); n > 0 {
			t.Errorf("a host listener (%s) received %d connection(s) or datagram(s) from the sandbox", key(ps[i]), n)
		}
	}
}

// Private, link-local, CGNAT and multicast destinations, the host's
// default router and the DNS forwarder's other ports are refused at once
// (a prohibit rule), not left to time out: a hostile agent cannot reach
// the LAN or cloud metadata, and a well-behaved CLI is not stalled.
func TestHostileNetworkPrivateRangesFailFast(t *testing.T) {
	needIsolated(t)
	ps := []netProbe{
		{"tcp", "169.254.169.254", 80}, // cloud metadata
		{"tcp", "10.255.255.1", 80},
		{"tcp", "172.16.0.1", 80},
		{"tcp", "192.168.0.1", 80},
		{"tcp", "100.64.0.1", 80}, // CGNAT, Tailscale
		{"tcp", "198.18.0.1", 80},
		{"tcp", "::ffff:192.168.0.1", 80},
		{"tcp", "fd00::1", 80},
		{"tcp", "fe80::1", 80},
		{"tcp", "64:ff9b::a00:1", 80}, // NAT64 of 10.0.0.1
		{"udp", "192.168.0.1", 53},
		{"udp", "224.0.0.251", 5353}, // mDNS to the LAN
		{"udp", "169.254.169.254", 80},
		{"tcp", DNSForward, 80}, // the forwarder answers DNS only
		{"tcp", DNSForward, 22},
		{"tcp", DNSForward, 853},
		{"udp", DNSForward, 123},
	}
	for _, gw := range defaultGateways() {
		ps = append(ps, netProbe{"tcp", gw, 80}, netProbe{"tcp", gw, 22}, netProbe{"udp", gw, 53})
	}
	r := isolatedRunner(t, probeScript("", ps))
	start := time.Now()
	_, err, seen := runHostile(t, r)
	if err != nil {
		t.Fatal(err)
	}
	got := probeResults(t, seen)
	for _, p := range ps {
		res, ok := got[key(p)]
		switch {
		case !ok:
			t.Errorf("%s: the probe did not run: %s", key(p), seen)
		case res.ok:
			t.Errorf("%s: reached (%+v)", key(p), res)
		case res.ms > 1000 || res.err == "timeout":
			t.Errorf("%s: took %d ms to fail (%s); it must be refused at once", key(p), res.ms, res.err)
		}
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("the run took %s", d)
	}
}

// online reports whether the host itself resolves and reaches host:443.
func online(host string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := net.DefaultResolver.LookupHost(ctx, host); err != nil {
		return err
	}
	c, err := (&net.Dialer{Timeout: 5 * time.Second}).Dial("tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return err
	}
	return c.Close()
}

// The CLIs need their APIs: names resolve through the forwarder and
// outbound TCP to public addresses works. Skipped when the host is offline.
func TestIsolatedNetworkReachesTheInternet(t *testing.T) {
	needIsolated(t)
	hosts := []string{"api.anthropic.com", "generativelanguage.googleapis.com"}
	for _, h := range hosts {
		if err := online(h); err != nil {
			t.Skipf("the host cannot reach %s itself (%v); offline", h, err)
		}
	}
	r := isolatedRunner(t, streamInit+`
cat /etc/resolv.conf >&2
python3 - >&2 <<'EOF'
import socket
for h in `+pyList(hosts)+`:
    try:
        addrs = {a[4][0] for a in socket.getaddrinfo(h, 443, proto=socket.IPPROTO_TCP)}
        print("RESOLVED", h)
    except OSError as e:
        print("NORESOLVE", h, e)
        continue
    try:
        socket.create_connection((h, 443), timeout=10).close()
        print("CONNECTED", h)
    except OSError as e:
        print("NOCONNECT", h, e)
EOF
`+streamResult)
	_, err, seen := runHostile(t, r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen, "nameserver "+DNSForward) {
		t.Errorf("the sandbox's resolver is not the forwarder: %s", seen)
	}
	for _, h := range hosts {
		for _, want := range []string{"RESOLVED " + h, "CONNECTED " + h} {
			if !strings.Contains(seen, want) {
				t.Errorf("want %q: %s", want, seen)
			}
		}
	}
}

// The agent tries to undo the isolation: flush or delete the rules, add a
// route or an address, take the interface down, load nftables or iptables
// rules, or make a network namespace of its own. Every attempt fails, it
// holds no capability, and afterwards the host's loopback and a private
// address are still out of reach.
func TestHostileNetworkCannotBeChanged(t *testing.T) {
	needIsolated(t)
	lo := tcpCounter(t, "127.0.0.1:0")
	if lo == nil {
		t.Skip("cannot listen on 127.0.0.1")
	}
	attempts := []string{
		"ip rule flush",
		"ip -6 rule flush",
		"ip rule del priority 200",
		"ip rule add priority 1 lookup main",
		"ip route add 10.9.9.0/24 dev lo",
		"ip route replace default dev lo",
		"ip addr add 10.1.1.1/32 dev lo",
		"ip link set lo down",
		"ip netns add escape",
		"/usr/sbin/nft flush ruleset",
		"/usr/sbin/nft add table inet escape",
		"/usr/sbin/iptables -F",
		"unshare -n true",
		"unshare -U -r -n true",
		"unshare -U -r -n -m true",
	}
	var pre strings.Builder
	pre.WriteString("grep -E '^Cap(Eff|Prm|Bnd|Amb):' /proc/self/status >&2\n")
	pre.WriteString("before=$(ip rule show; ip -6 rule show)\n")
	for _, a := range attempts {
		fmt.Fprintf(&pre, "%s >/dev/null 2>&1 && echo %q >&2\n", a, "ALTERED "+a)
	}
	pre.WriteString(`[ "$before" = "$(ip rule show; ip -6 rule show)" ] || echo "ALTERED the rules changed" >&2` + "\n")
	ps := []netProbe{{"tcp", "127.0.0.1", lo.port}, {"tcp", "10.255.255.1", 80}}
	r := isolatedRunner(t, probeScript(pre.String(), ps))
	_, err, seen := runHostile(t, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(seen, "\n") {
		if strings.HasPrefix(line, "ALTERED") {
			t.Errorf("the agent changed the network: %s", line)
		}
		if f := strings.Fields(line); len(f) == 2 && strings.HasPrefix(f[0], "Cap") && strings.Trim(f[1], "0") != "" {
			t.Errorf("the agent holds capabilities: %s", line)
		}
	}
	if !strings.Contains(seen, "CapEff:") {
		t.Errorf("capabilities not reported: %s", seen)
	}
	got := probeResults(t, seen)
	for _, p := range ps {
		if res, ok := got[key(p)]; !ok || res.ok {
			t.Errorf("%s after the attempts: %+v (ran %v)", key(p), res, ok)
		}
	}
	if n := lo.hits.Load(); n > 0 {
		t.Errorf("the host's loopback received %d connection(s)", n)
	}
}

// Nothing is forwarded into the sandbox: a service the agent starts on
// every address inside cannot be reached from the host's loopback or
// addresses, over TCP or UDP, while it listens.
func TestHostileNetworkNoInboundForward(t *testing.T) {
	needIsolated(t)
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Skip(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // free on the host, so a pasta forward would be the only listener there
	r := isolatedRunner(t, streamInit+fmt.Sprintf(`
python3 - >&2 <<'EOF'
import socket, time
socks = []
for fam, addr in ((socket.AF_INET6, "::"), (socket.AF_INET, "0.0.0.0")):
    for typ in (socket.SOCK_STREAM, socket.SOCK_DGRAM):
        s = socket.socket(fam, typ)
        s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        if fam == socket.AF_INET6:
            s.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
        s.bind((addr, %d))
        if typ == socket.SOCK_STREAM:
            s.listen(8)
        s.setblocking(False)
        socks.append((typ, s))
print("LISTENING")
end = time.time() + 4
while time.time() < end:
    for typ, s in socks:
        try:
            if typ == socket.SOCK_STREAM:
                s.accept()[0].close()
            else:
                s.recvfrom(64)
            print("INBOUND reached the sandbox")
        except BlockingIOError:
            pass
    time.sleep(0.05)
EOF
`, port)+streamResult)
	targets := append([]string{"127.0.0.1", "::1"}, hostAddrs(t)...)
	done := make(chan struct{})
	var reached atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			case <-time.After(100 * time.Millisecond):
			}
			for _, a := range targets {
				hp := net.JoinHostPort(a, strconv.Itoa(port))
				if c, err := net.DialTimeout("tcp", hp, 200*time.Millisecond); err == nil {
					reached.Add(1)
					c.Close()
				}
				if c, err := net.Dial("udp", hp); err == nil {
					c.Write([]byte("host"))
					c.Close()
				}
			}
		}
	}()
	_, err, seen := runHostile(t, r)
	close(done)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen, "LISTENING") {
		t.Fatalf("the sandbox's listener did not start: %s", seen)
	}
	if strings.Contains(seen, "INBOUND") || reached.Load() > 0 {
		t.Errorf("the host reached the sandbox's port %d (%d TCP connects): %s", port, reached.Load(), seen)
	}
}

// The isolated chain on the command line: pasta with no port forwarded in
// either direction and nothing mapped to the host, then the setup shell,
// then bwrap told the operator's ids and to drop every capability, with the
// forwarder as its resolver and the network namespace shared from pasta.
func TestIsolatedArgv(t *testing.T) {
	n := Network{Mode: NetIsolated, Pasta: "/usr/bin/pasta", Flock: "/usr/bin/flock", IP: "/usr/bin/ip",
		Host: []netip.Prefix{netip.MustParsePrefix("203.0.113.7/32"), netip.MustParsePrefix("2001:db8::/64")}}
	w := Workdir{Root: "/w"}
	sb := testSandbox(t, "")
	sb.Net = n
	argv := sb.Argv(w, "/out/x.strace", []string{sb.Exe()})
	sb.Strace = "/usr/bin/strace"
	withStrace := sb.Argv(w, "/out/x.strace", []string{sb.Exe()})
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"/usr/bin/pasta --config-net --quiet -t none -u none -T none -U none --map-host-loopback none --map-guest-addr none --dns-forward " + DNSForward + " -- /bin/sh -c ",
		"to 169.254.0.0/16 prohibit", "to 192.168.0.0/16 prohibit", "to fc00::/7 prohibit", "to 203.0.113.7/32 prohibit", "to 2001:db8::/64 prohibit",
		"to " + DNSForward + " ipproto udp dport 53 lookup main",
		" sh /w/lifeline /usr/bin/bwrap ",
		"--ro-bind /w/etc/resolv.conf ",
		"--unshare-all --share-net --unshare-user --disable-userns --uid " + strconv.Itoa(os.Getuid()) + " --gid " + strconv.Itoa(os.Getgid()) + " --cap-drop ALL",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv lacks %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(strings.Join(withStrace, " "), " sh /w/lifeline /usr/bin/strace -f ") {
		t.Errorf("strace must run inside the namespace, after the setup: %v", withStrace)
	}
	if got := Lifeline(n, w); got != "/w/lifeline" {
		t.Errorf("lifeline = %q", got)
	}
	host := testSandbox(t, "")
	if a := host.Argv(w, "", []string{host.Exe()}); a[0] != "/usr/bin/bwrap" || strings.Contains(strings.Join(a, " "), "pasta") {
		t.Errorf("host network argv = %v", a)
	}
	if got := Lifeline(host.Net, w); got != "" {
		t.Errorf("host network lifeline = %q", got)
	}
}

// A missing pasta, or any missing tool, refuses the isolated network with
// the fix, never falling back to the host's; RunTask refuses a Network that
// was never resolved; an unknown mode is refused.
func TestIsolatedNetworkRefusesWithoutPasta(t *testing.T) {
	noHost := func() ([]netip.Prefix, error) { return nil, nil }
	missing := func(name string) func(string) (string, error) {
		return func(n string) (string, error) {
			if n == name || strings.HasSuffix(n, "/"+name) {
				return "", exec.ErrNotFound
			}
			return "/usr/bin/" + n, nil
		}
	}
	n, err := resolveNetwork(NetIsolated, missing("pasta"), noHost)
	if err == nil || !strings.Contains(err.Error(), "sudo apt install -y passt") || !strings.Contains(err.Error(), "-net host") {
		t.Errorf("err = %v", err)
	}
	if n.Isolated() && n.Check() == nil {
		t.Error("a network without pasta passes Check")
	}
	for _, tool := range []string{"flock", "ip"} {
		if _, err := resolveNetwork(NetIsolated, missing(tool), noHost); err == nil || !strings.Contains(err.Error(), tool) {
			t.Errorf("missing %s: err = %v", tool, err)
		}
	}
	if _, err := resolveNetwork("bridge", missing(""), noHost); err == nil {
		t.Error("an unknown mode was accepted")
	}
	if _, err := ParseNetMode("Host"); err == nil {
		t.Error("ParseNetMode accepted a mode it does not know")
	}
	if n, err := resolveNetwork(NetHost, missing("pasta"), noHost); err != nil || n.Isolated() {
		t.Errorf("host mode needs no pasta: %+v %v", n, err)
	}
	if err := (Network{}).Check(); err == nil {
		t.Error("the zero Network (isolated, unresolved) passes Check")
	}
	r := testRunner(t, testSandbox(t, "#!/bin/sh\nexit 0\n"))
	r.Sandbox.Net = Network{}
	if _, err := r.RunTask(context.Background(), oneTurnTask, "technical"); err == nil || !strings.Contains(err.Error(), "pasta") {
		t.Errorf("RunTask on an unresolved network: %v", err)
	}
}

// HostPrefixes covers the host's own addresses and on-link prefixes, never
// loopback, and nothing blocked already covers.
func TestHostPrefixes(t *testing.T) {
	ps, err := HostPrefixes()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Addr().IsLoopback() {
			t.Errorf("loopback in %v", ps)
		}
	}
	for _, a := range hostAddrs(t) {
		ip := netip.MustParseAddr(a)
		covered := false
		for _, p := range append(append([]netip.Prefix(nil), blocked...), ps...) {
			covered = covered || p.Contains(ip)
		}
		if !covered {
			t.Errorf("host address %s is not refused (host prefixes %v)", a, ps)
		}
	}
	got := minusBlocked([]netip.Prefix{netip.MustParsePrefix("192.168.1.5/32"), netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8::1/128")})
	if fmt.Sprint(got) != "[203.0.113.0/24 2001:db8::1/128]" {
		t.Errorf("minusBlocked = %v", got)
	}
}

// The driver killed outright (SIGKILL, no chance to clean up) takes the
// sandbox with it: the lifeline it held is released, and the sandbox, which
// was waiting on it, kills itself. Run on both networks.
func TestHostileDriverKilledTakesTheSandbox(t *testing.T) {
	if os.Getenv("AGENTPROBE_TEST_DRIVER") != "" {
		return // the helper process below runs TestHelperDriver instead
	}
	needBwrap(t)
	if Prlimit() == "" {
		testenv.Unavailable(t, "prlimit unavailable")
	}
	for _, mode := range []NetMode{NetHost, NetIsolated} {
		t.Run(string(mode), func(t *testing.T) {
			if mode == NetIsolated {
				needIsolated(t)
			}
			tag := strconv.FormatInt(time.Now().UnixNano(), 36)
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperDriver$", "-test.v")
			cmd.Env = append(os.Environ(), "AGENTPROBE_TEST_DRIVER="+string(mode), "AGENTPROBE_TEST_TAG="+tag)
			out, _ := os.CreateTemp(t.TempDir(), "helper")
			cmd.Stdout, cmd.Stderr = out, out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(20 * time.Second)
			for len(processesWith("agentprobe-orphan-"+tag+"-cli")) == 0 {
				if time.Now().After(deadline) {
					cmd.Process.Kill()
					cmd.Wait()
					b, _ := os.ReadFile(out.Name())
					t.Fatalf("the sandbox never started: %s", b)
				}
				time.Sleep(50 * time.Millisecond)
			}
			cmd.Process.Signal(syscall.SIGKILL)
			cmd.Wait()
			assertNoOrphans(t, tag)
		})
	}
}

// TestHelperDriver is the driver TestHostileDriverKilledTakesTheSandbox
// kills; it does nothing in a normal test run.
func TestHelperDriver(t *testing.T) {
	mode, tag := NetMode(os.Getenv("AGENTPROBE_TEST_DRIVER")), os.Getenv("AGENTPROBE_TEST_TAG")
	if mode == "" {
		t.Skip("helper process only")
	}
	r := hostileRunner(t, streamInit+orphanScript(tag)+"exec -a agentprobe-orphan-"+tag+"-cli sleep 300\n")
	if mode == NetIsolated {
		r.Sandbox.Net = needIsolated(t)
	} else {
		r.Sandbox.Net = Network{Mode: NetHost}
	}
	r.TurnTimeout = 5 * time.Minute
	runHostile(t, r)
}
