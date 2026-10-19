package claudecode

import (
	"encoding/json"
	"net/netip"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"synapseos/internal/testenv"
)

// The isolated network is several defences in depth: the prohibit rules for
// private ranges (blocked), the rules for the host's own addresses
// (Network.Host), and pasta told to map nothing to the host
// (--no-map-gw). The hostile tests in
// hostile_network_test.go attack them together, so with one layer gone the
// others still pass them: removing the Host rules or --no-map-gw
// failed only TestIsolatedArgv. The tests here take the other layers away
// (Network.chain with fewer rules, pasta's argv with a flag dropped) and show
// that each layer stops what it is for on its own, against a control run
// without it that the same probe sees through.
//
// The private-range rules are pinned behaviourally by the hostile tests
// already: TestHostileNetworkHostUnreachable dials every host address
// (docker0's included, which pasta reaches when no rule refuses it) and
// TestHostileNetworkPrivateRangesFailFast wants a refusal, not a timeout.
//
// --map-guest-addr is no longer passed. Older pasta (Ubuntu 24.04's, which
// CI runs) rejects it as unknown, and on the release this was written on
// (Debian passt 0.0~git20250503) passing "none" changed nothing observable,
// because its default is already none.

// layerProbe runs python3 in the namespace that n.chain(lifeline, refuse)
// builds, with the pasta flags in drop (each followed by its value) left
// out, and returns what each probe saw, as probeScript reports it.
// valueless are the pasta flags in the chain that take no value.
var valueless = map[string]bool{"--no-map-gw": true}

func layerProbe(t *testing.T, n Network, refuse []netip.Prefix, drop []string, ps []netProbe) map[string]probeResult {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("python3 is needed to probe the namespace and is not on PATH")
	}
	lifeline := filepath.Join(t.TempDir(), "lifeline")
	h, err := HoldLifeline(lifeline)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	var argv []string
	full := n.chain(lifeline, refuse)
	removed := 0
	for i := 0; i < len(full); i++ {
		if slices.Contains(drop, full[i]) {
			removed++
			if !valueless[full[i]] {
				i++ // and its value
				removed++
			}
			continue
		}
		argv = append(argv, full[i])
	}
	if len(argv) != len(full)-removed || removed == 0 && len(drop) > 0 {
		t.Fatalf("not every flag in %v is in the chain: %v", drop, full)
	}
	js, _ := json.Marshal(ps)
	argv = append(argv, py, "-c", layerPy, string(js))
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		t.Fatalf("the namespace did not run (%v): %s", err, out)
	}
	return probeResults(t, string(out))
}

// layerPy is probeScript's prober on its own: one line per probe, "PROBE
// kind addr port ok=0|1 err=E ms=N", 3 s timeout.
const layerPy = `
import errno, json, socket, sys, time
for p in json.loads(sys.argv[1]):
    kind, addr, port = p["kind"], p["addr"], p["port"]
    fam = socket.AF_INET6 if ":" in addr else socket.AF_INET
    s = socket.socket(fam, socket.SOCK_DGRAM if kind == "udp" else socket.SOCK_STREAM)
    s.settimeout(3)
    t, ok, err = time.monotonic(), 0, "-"
    try:
        if kind == "udp":
            s.sendto(b"agent", (addr, port))
        else:
            s.connect((addr, port))
        ok = 1
    except OSError as e:
        err = errno.errorcode.get(e.errno, str(e)) if e.errno else type(e).__name__
    finally:
        s.close()
    print("PROBE %s %s %d ok=%d err=%s ms=%d" % (kind, addr, port, ok, err, (time.monotonic() - t) * 1000), flush=True)
`

// The rules for the host's own addresses refuse what they name at once,
// with no private-range rule involved: 203.0.113.0/24 (TEST-NET-3) is in no
// blocked range, so only a Host rule can refuse it. Without one the same
// probe is not refused by the namespace (it times out or the outside
// answers), which shows the refusal is the Host rule's. The real host's
// addresses are mostly private ranges blocked already covers; TEST-NET
// stands in for one that is not (a public IPv6 prefix, a VPN's address).
func TestNetworkHostRulesRefuseOnTheirOwn(t *testing.T) {
	n := needIsolated(t)
	target := netProbe{"tcp", "203.0.113.7", 9}
	if slices.ContainsFunc(blocked, func(b netip.Prefix) bool { return b.Contains(netip.MustParseAddr(target.Addr)) }) {
		t.Fatalf("%s is in a blocked range; it cannot isolate the Host rules", target.Addr)
	}
	n.Host = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	res := layerProbe(t, n, blocked, nil, []netProbe{target})[key(target)]
	if res.ok || res.err != "EACCES" || res.ms > 1000 {
		t.Errorf("with a Host rule for %s: %+v; want refused at once (EACCES, from the prohibit rule)", target.Addr, res)
	}
	n.Host = nil
	if ctl := layerProbe(t, n, blocked, nil, []netProbe{target})[key(target)]; ctl.err == "EACCES" {
		t.Errorf("control: with no Host rule %s is still refused (%+v), so the refusal above did not come from the Host rule", target.Addr, ctl)
	}
}

// --no-map-gw on its own keeps the host's loopback out of
// reach. pasta's default maps the namespace's default gateway to the host's
// 127.0.0.1, so a service bound there (Ollama by default) answers an agent
// that dials the gateway. The gateway is in a private range blocked covers,
// which hid this layer: here no private-range rule is installed. The
// control drops the flag and must reach the listener, or the test proves
// nothing on this host. IPv4 only: the IPv6 gateway is link-local.
func TestNetworkNoHostLoopbackMapOnItsOwn(t *testing.T) {
	n := needIsolated(t)
	gws := defaultGateways()
	if len(gws) == 0 {
		testenv.Unavailable(t, "no default IPv4 gateway, which pasta maps to the host's loopback by default")
	}
	lo := tcpCounter(t, "127.0.0.1:0")
	if lo == nil {
		t.Fatal("cannot listen on 127.0.0.1")
	}
	ps := []netProbe{{"tcp", gws[0], lo.port}}
	n.Host = nil
	res := layerProbe(t, n, nil, nil, ps)[key(ps[0])]
	time.Sleep(100 * time.Millisecond)
	if res.ok || lo.hits.Load() != 0 {
		t.Errorf("with --no-map-gw and no private-range rule, the gateway %s reached the host's loopback: %+v, %d hits", gws[0], res, lo.hits.Load())
	}
	before := lo.hits.Load()
	ctl := layerProbe(t, n, nil, []string{"--no-map-gw"}, ps)[key(ps[0])]
	time.Sleep(100 * time.Millisecond)
	if !ctl.ok || lo.hits.Load() == before {
		t.Errorf("control: without --no-map-gw the gateway %s did not reach the host's loopback (%+v); this host cannot show the layer", gws[0], ctl)
	}
}
