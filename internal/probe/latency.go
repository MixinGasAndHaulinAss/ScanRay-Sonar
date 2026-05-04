// latency.go — ICMP latency probe to operator-defined targets (8.8.8.8
// by default) and the host's default gateway.
//
// We send 4 ICMP echo requests 200 ms apart, listen 1 s for replies,
// and report min/avg/max/lossPct. The probe binary runs as root /
// SYSTEM on every install path (`scripts/build-probe.sh` install
// one-liners register a systemd unit on Linux and an SCM service as
// Local System on Windows), so the raw socket is available.
//
// In sandboxed environments (containers without `cap_net_raw`, locked
// down macOS sandboxes), opening the listener fails. We surface that
// as an error to the caller, who logs once per probe lifetime via
// CollectionWarnings — we don't want to spam the warning list every
// 60 seconds.

package probe

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"runtime"
	"sort"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// LatencyRow is one (probe, target) result. Always populated for every
// target even on total loss — AvgMs/MinMs/MaxMs are zero and LossPct
// is 100 in that case so the UI can still render the row.
type LatencyRow struct {
	Target  string  `json:"target"` // logical name: "8.8.8.8" or "gateway"
	Address string  `json:"address,omitempty"`
	AvgMs   float64 `json:"avgMs"`
	MinMs   float64 `json:"minMs"`
	MaxMs   float64 `json:"maxMs"`
	LossPct float64 `json:"lossPct"`
}

// ProbeICMP sends 4 ICMP echo requests to addr and returns aggregate
// statistics. addr must be a literal IPv4 address; resolution is the
// caller's responsibility (they likely have a target name already
// resolved by the gateway lookup or a DNS resolver).
//
// Total wall-clock cost is bounded at ~1.6 s (4 echos, 200 ms apart,
// then a 1 s drain to catch late replies). ctx cancellation cuts in
// between iterations.
func ProbeICMP(ctx context.Context, addr string) (LatencyRow, error) {
	row := LatencyRow{Target: addr, Address: addr, LossPct: 100}

	ip := net.ParseIP(addr)
	if ip == nil || ip.To4() == nil {
		return row, fmt.Errorf("latency: %q is not a valid IPv4 address", addr)
	}

	conn, err := openICMPConn()
	if err != nil {
		return row, err
	}
	defer conn.Close()

	const tries = 4
	const gap = 200 * time.Millisecond
	const replyWait = 1 * time.Second

	rtts := make([]time.Duration, 0, tries)
	id := os.Getpid() & 0xffff
	dst := &net.IPAddr{IP: ip}

	for i := 0; i < tries; i++ {
		if ctx.Err() != nil {
			break
		}
		seq := i + 1
		body := makeEchoBody(id, seq)
		msg := icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{ID: id, Seq: seq, Data: body},
		}
		out, err := msg.Marshal(nil)
		if err != nil {
			continue
		}
		start := time.Now()
		if _, err := conn.WriteTo(out, dst); err != nil {
			continue
		}

		// Drain replies until we see our (id, seq) or the per-iteration
		// budget elapses. Wrong-id replies (other pingers on the host)
		// get discarded silently.
		deadline := time.Now().Add(replyWait)
		_ = conn.SetReadDeadline(deadline)
		buf := make([]byte, 1500)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				break
			}
			parsed, err := icmp.ParseMessage(1 /*protocolICMP*/, buf[:n])
			if err != nil {
				continue
			}
			if parsed.Type != ipv4.ICMPTypeEchoReply {
				continue
			}
			echo, ok := parsed.Body.(*icmp.Echo)
			if !ok {
				continue
			}
			if echo.ID != id || echo.Seq != seq {
				continue
			}
			rtts = append(rtts, time.Since(start))
			break
		}

		if i < tries-1 {
			select {
			case <-ctx.Done():
				goto summarize
			case <-time.After(gap):
			}
		}
	}

summarize:
	if len(rtts) == 0 {
		row.LossPct = 100
		return row, nil
	}
	sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })
	var sum time.Duration
	for _, r := range rtts {
		sum += r
	}
	row.MinMs = round1(float64(rtts[0]) / float64(time.Millisecond))
	row.MaxMs = round1(float64(rtts[len(rtts)-1]) / float64(time.Millisecond))
	row.AvgMs = round1(float64(sum) / float64(len(rtts)) / float64(time.Millisecond))
	row.LossPct = round1(float64(tries-len(rtts)) / float64(tries) * 100)
	return row, nil
}

// openICMPConn opens a privileged ICMP socket. On Linux + macOS we use
// "ip4:icmp" which needs CAP_NET_RAW (or root). On Windows the raw
// socket type is "ip4:icmp" too; SYSTEM has the access right.
//
// Some Linux distros allow non-privileged ICMP via "udp4" + the
// net.ipv4.ping_group_range sysctl; we do not rely on that path
// because the probe always runs privileged.
func openICMPConn() (*icmp.PacketConn, error) {
	c, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		// Annotate the error with platform context so operators can
		// figure out why ICMP doesn't work without diving into source.
		return nil, fmt.Errorf("latency: ICMP listen failed (need raw-socket on %s): %w",
			runtime.GOOS, err)
	}
	return c, nil
}

func makeEchoBody(id, seq int) []byte {
	// 16 bytes: timestamp (8) + id (4) + seq (4). The exact contents
	// don't matter to ICMP, but a known body length keeps wire sizes
	// uniform and lets us correlate echos in PCAPs without parsing.
	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf[0:8], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint32(buf[8:12], uint32(id))
	binary.BigEndian.PutUint32(buf[12:16], uint32(seq))
	return buf
}

func round1(v float64) float64 {
	// Round to one decimal place. We avoid math.Round to keep the
	// import surface small.
	if v >= 0 {
		return float64(int(v*10+0.5)) / 10
	}
	return float64(int(v*10-0.5)) / 10
}

