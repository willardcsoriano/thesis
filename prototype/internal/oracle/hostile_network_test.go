package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The oracle's sandboxes have no network at all (--unshare-all, no
// --share-net): the default one the corpus runs in, and HideHost, which
// probe.Trace (the API driver's per-step runner) and the scorer's replay
// use. A command that tries the host's loopback, the host's addresses and
// an abstract unix socket reaches none of them; the host's listeners count
// what arrives.
func TestSandboxesCannotReachTheHost(t *testing.T) {
	bw := Bwrap()
	if bw == "" {
		t.Skip("bubblewrap unavailable")
	}
	var hits atomic.Int32
	type target struct {
		Kind, Addr string
		Port       int
	}
	var targets []target
	accept := func(ln net.Listener) {
		t.Cleanup(func() { ln.Close() })
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				hits.Add(1)
				c.Close()
			}
		}()
	}
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0", ":0"} {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		accept(ln)
		port := ln.Addr().(*net.TCPAddr).Port
		switch addr {
		case "127.0.0.1:0":
			targets = append(targets, target{"tcp", "127.0.0.1", port})
		case "[::1]:0":
			targets = append(targets, target{"tcp", "::1", port})
		default:
			addrs, _ := net.InterfaceAddrs()
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok {
					if ip, ok := netip.AddrFromSlice(ipn.IP); ok && !(ip.Unmap().Is6() && ip.IsLinkLocalUnicast()) {
						targets = append(targets, target{"tcp", ip.Unmap().String(), port})
					}
				}
			}
		}
	}
	name := "oracle-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if ln, err := net.Listen("unix", "@"+name); err == nil {
		accept(ln)
		targets = append(targets, target{"unix", name, 0})
	}
	if len(targets) == 0 {
		t.Skip("no host listener could be opened")
	}
	js, _ := json.Marshal(targets)
	cmd := `python3 - <<'EOF' > probes
import json, socket
for p in json.loads(` + strconv.Quote(string(js)) + `):
    if p["Kind"] == "unix":
        s, target = socket.socket(socket.AF_UNIX), "\0" + p["Addr"]
    else:
        s = socket.socket(socket.AF_INET6 if ":" in p["Addr"] else socket.AF_INET)
        target = (p["Addr"], p["Port"])
    s.settimeout(3)
    try:
        s.connect(target)
        print("REACHED", p["Kind"], p["Addr"], p["Port"])
    except OSError as e:
        print("REFUSED", p["Kind"], p["Addr"], p["Port"], e)
    s.close()
EOF`
	for _, sb := range []Sandbox{{}, {HideHost: true}} {
		t.Run(fmt.Sprintf("hide=%v", sb.HideHost), func(t *testing.T) {
			root := t.TempDir()
			oc, err := ExecuteIn(context.Background(), bw, root, cmd, 60*time.Second, sb)
			if err != nil || !oc.Clean() {
				t.Fatalf("%v %+v", err, oc)
			}
			b, _ := os.ReadFile(filepath.Join(root, "probes"))
			out := string(b)
			if strings.Contains(out, "REACHED") {
				t.Errorf("the sandbox reached the host:\n%s", out)
			}
			if n := strings.Count(out, "REFUSED"); n != len(targets) {
				t.Errorf("%d of %d probes ran:\n%s", n, len(targets), out)
			}
		})
	}
	if n := hits.Load(); n > 0 {
		t.Errorf("host listeners received %d connection(s) from the sandboxes", n)
	}
}
