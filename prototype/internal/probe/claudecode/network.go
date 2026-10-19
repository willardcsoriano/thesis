package claudecode

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
)

// NetMode is how the sandbox reaches the network.
type NetMode string

const (
	// NetIsolated gives the sandbox its own network namespace, connected
	// by pasta: outbound connections to public addresses work, while the
	// host's loopback, the host's other addresses, the LAN and every
	// private or link-local range are refused, and no port is forwarded
	// either way. It is the zero value's meaning too, so a Sandbox built
	// without a mode is isolated or refused, never on the host's network.
	NetIsolated NetMode = "isolated"
	// NetHost shares the host's network namespace (bubblewrap's
	// --share-net): the agent reaches every service listening on the
	// host's loopback and the LAN. Only for an operator who asks for it.
	NetHost NetMode = "host"
)

// ParseNetMode reads the -net flag.
func ParseNetMode(s string) (NetMode, error) {
	switch m := NetMode(s); m {
	case NetIsolated, NetHost:
		return m, nil
	}
	return "", fmt.Errorf("-net %q: want %s or %s", s, NetIsolated, NetHost)
}

// DNSForward is the only nameserver the isolated sandbox knows (its
// resolv.conf). pasta maps DNS traffic sent to it onto the host's first
// nameserver; the namespace's rules let only port 53 to it through, so it
// is not a way to the host's other services.
const DNSForward = "169.254.0.53"

// blocked are the destinations refused whatever the host's addresses:
// "this network", RFC 1918, shared address space (CGNAT, Tailscale),
// link-local (cloud metadata at 169.254.169.254), IETF protocol
// assignments, benchmarking (where some proxies put fake-IP DNS),
// multicast and reserved space (broadcast included); for IPv6, unique
// local, link-local, site-local, multicast and the NAT64 prefixes, which
// embed IPv4 addresses. 127.0.0.0/8 and ::1 are not listed: in the
// namespace they are its own loopback (the local routing table answers
// them before any rule), and with no port forwarded nothing there leads to
// the host.
var blocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// Network is the sandbox's network: host, or isolated with the tools that
// build the namespace. ResolveNetwork fills it.
type Network struct {
	Mode NetMode // "" means NetIsolated
	// Isolated only: the binaries, run on the host side of the sandbox,
	// in the namespace pasta creates and before bubblewrap.
	Pasta, Flock, IP string
	// Host are the host's own addresses and on-link prefixes not already
	// in blocked, read when the run starts. They are refused too, so the
	// agent cannot reach the host by an address of another interface
	// (docker0, a VPN) or a LAN neighbour on a public prefix (IPv6).
	Host []netip.Prefix
}

// Isolated says whether the sandbox gets its own network namespace.
func (n Network) Isolated() bool { return n.Mode != NetHost }

// Check refuses a Network that cannot be built: an unknown mode, or
// isolated without its tools. RunTask calls it, so nothing runs on a
// network the operator did not choose.
func (n Network) Check() error {
	switch n.Mode {
	case NetHost:
		return nil
	case "", NetIsolated:
	default:
		return fmt.Errorf("unknown network mode %q", n.Mode)
	}
	for name, p := range map[string]string{"pasta": n.Pasta, "flock": n.Flock, "ip": n.IP} {
		if !strings.HasPrefix(p, "/") {
			return fmt.Errorf("isolated network: %s is not resolved (%s)", name, missingPasta)
		}
	}
	return nil
}

const missingPasta = "install pasta with `sudo apt install -y passt`, or pass -net host to share the host's network, which lets the agent reach the host's loopback services and the LAN"

// ResolveNetwork finds what mode needs. For NetIsolated it finds pasta,
// flock and ip and reads the host's addresses; a missing tool is
// an error naming the fix, never a fall back to the host's network. Probe
// then checks that the namespace can actually be built here.
func ResolveNetwork(mode NetMode) (Network, error) {
	return resolveNetwork(mode, exec.LookPath, HostPrefixes)
}

func resolveNetwork(mode NetMode, look func(string) (string, error), host func() ([]netip.Prefix, error)) (Network, error) {
	n := Network{Mode: mode}
	switch mode {
	case NetHost:
		return n, nil
	case NetIsolated:
	default:
		return n, fmt.Errorf("unknown network mode %q", mode)
	}
	var err error
	if n.Pasta, err = look("pasta"); err != nil {
		return n, fmt.Errorf("the isolated network needs pasta, which is not installed: %s", missingPasta)
	}
	for _, t := range []struct {
		name string
		dst  *string
		alt  []string
	}{
		{"flock", &n.Flock, nil},
		{"ip", &n.IP, []string{"/usr/sbin/ip", "/sbin/ip", "/usr/bin/ip", "/bin/ip"}},
	} {
		p, err := look(t.name)
		for _, a := range t.alt {
			if err == nil {
				break
			}
			p, err = look(a) // a path with a slash is tried as it is
		}
		if err != nil {
			return n, fmt.Errorf("the isolated network needs %s (util-linux or iproute2), which is not on PATH", t.name)
		}
		*t.dst = p
	}
	if n.Host, err = host(); err != nil {
		return n, fmt.Errorf("reading the host's addresses: %w", err)
	}
	return n, nil
}

// Probe runs the isolated namespace's setup with /bin/true inside, under a
// held lifeline, and reports why it failed, so a host where it cannot work
// is refused before the first task. A host-mode Network passes.
func (n Network) Probe() error {
	if !n.Isolated() {
		return nil
	}
	if err := n.Check(); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "agentprobe-net-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	lifeline := dir + "/lifeline"
	l, err := HoldLifeline(lifeline)
	if err != nil {
		return err
	}
	defer l.Close()
	argv := append(n.wrap(lifeline), "/bin/true")
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("the isolated network could not be set up (%v): %s", err, OneLine(string(out)))
	}
	return nil
}

// HostPrefixes are the host's addresses (as /32 or /128) and their on-link
// prefixes, loopback excluded, minus whatever blocked already covers,
// sorted.
func HostPrefixes() ([]netip.Prefix, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []netip.Prefix
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			return nil, err
		}
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
			bits, size := ipn.Mask.Size()
			if ip.Is4() && size == 128 {
				bits -= 96
			}
			if ip.IsLoopback() {
				continue
			}
			out = append(out, netip.PrefixFrom(ip, ip.BitLen()))
			if p, err := ip.Prefix(bits); err == nil {
				out = append(out, p)
			}
		}
	}
	return minusBlocked(out), nil
}

// minusBlocked sorts ps, drops duplicates and every prefix inside a
// blocked one.
func minusBlocked(ps []netip.Prefix) []netip.Prefix {
	var out []netip.Prefix
	for _, p := range ps {
		if !slices.ContainsFunc(blocked, func(b netip.Prefix) bool { return b.Bits() <= p.Bits() && b.Contains(p.Addr()) }) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b netip.Prefix) int {
		if c := a.Addr().Compare(b.Addr()); c != 0 {
			return c
		}
		return a.Bits() - b.Bits()
	})
	return slices.Compact(out)
}

// wrap is the isolated chain up to the program it starts in the namespace:
//
//	pasta: a new user and network namespace, the host's address and
//	  default route copied in (--config-net), no port forwarded in either
//	  direction (-t/-u/-T/-U none), no address mapped to the host's
//	  loopback or to the host's own address, DNS to DNSForward forwarded to
//	  the host's nameserver;
//	sh: the namespace's first process, as its root, adds the routing rules
//	  (ip rule prohibit, which refuses at once rather than timing out),
//	  then starts the program and kills it as soon as it can take a shared
//	  lock on lifeline. The driver holds that lock exclusively until the
//	  turn ends or it kills pasta (Exec releases it first), and the kernel
//	  drops it if the driver is killed outright. The lock is the only way
//	  the namespace learns that it should end: pasta does not kill the
//	  process it started when pasta dies, and a parent-death signal set on
//	  that process never arrives (it is the first process of its own PID
//	  namespace, and its parent lies outside it). When sh ends, every
//	  process in the namespace is killed with it.
//
// The rules belong to the namespace's owner; the agent, a non-root user
// with no capabilities in a user namespace nested below it, cannot change
// them (TestHostileNetworkCannotBeChanged).
func (n Network) wrap(lifeline string) []string { return n.chain(lifeline, blocked) }

// chain is wrap with refuse in place of blocked, so a test can take one
// layer of the isolation away and show that another holds on its own
// (network_layers_test.go). Everything else calls wrap.
func (n Network) chain(lifeline string, refuse []netip.Prefix) []string {
	var b strings.Builder
	ip := n.IP
	b.WriteString("set -e; ")
	for i, proto := range []string{"udp", "tcp"} {
		fmt.Fprintf(&b, "%s -4 rule add priority %d to %s ipproto %s dport 53 lookup main; ", ip, 100+i, DNSForward, proto)
	}
	var v6 strings.Builder
	for _, p := range append(slices.Clone(refuse), n.Host...) {
		if p.Addr().Is4() {
			fmt.Fprintf(&b, "%s -4 rule add priority 200 to %s prohibit; ", ip, p)
		} else {
			fmt.Fprintf(&v6, "%s -6 rule add priority 200 to %s prohibit; ", ip, p)
		}
	}
	// No IPv6 in the namespace (the kernel has it off) leaves nothing to refuse.
	fmt.Fprintf(&b, "if [ -d /proc/sys/net/ipv6 ]; then %s:; fi; ", v6.String())
	fmt.Fprintf(&b, `l=$1; shift; "$@" & c=$!; (set +e; %s -s "$l" true; kill -KILL "$c") & wait "$c"`, n.Flock)
	return []string{n.Pasta, "--config-net", "--quiet",
		"-t", "none", "-u", "none", "-T", "none", "-U", "none",
		"--map-host-loopback", "none", "--map-guest-addr", "none",
		"--dns-forward", DNSForward,
		"--", "/bin/sh", "-c", b.String(), "sh", lifeline}
}

// Held is a lock on a lifeline, held by the driver while a sandbox it
// started runs.
type Held struct{ f *os.File }

// HoldLifeline creates path if needed and takes an exclusive lock on it,
// which the isolated sandbox started with that path waits on. It fails if
// another process holds it. The descriptor is close-on-exec, so no child
// keeps the lock alive after the driver dies.
func HoldLifeline(path string) (*Held, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("lifeline %s is held by another process", path)
		}
		return nil, err
	}
	return &Held{f}, nil
}

// Close releases the lock; a sandbox still waiting on it is killed. A
// second Close does nothing.
func (l *Held) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	return f.Close()
}

// Lifeline is the path an isolated sandbox in w waits on, or "" for the
// host network, which has none.
func Lifeline(n Network, w Workdir) string {
	if !n.Isolated() {
		return ""
	}
	return w.Lifeline()
}
