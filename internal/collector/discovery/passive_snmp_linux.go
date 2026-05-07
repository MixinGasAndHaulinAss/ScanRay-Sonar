//go:build linux

// Linux capture of UDP/161 destination IPs using a raw AF_PACKET
// socket with a kernel-side BPF filter. This is the same mechanism
// `tcpdump udp dst port 161` uses; doing it directly avoids shelling
// out to tcpdump and parsing its text output.
//
// We deliberately avoid gopacket here. gopacket is great but pulls in
// a sizeable transitive graph and (on Linux) usually depends on
// libpcap. For our needs — read frames, find IPv4+UDP, harvest dst —
// the parsing is a few dozen lines and a clean cgo-free dependency
// matters more than the convenience of gopacket's layer model.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

// CapturePassiveSNMP is the Linux entry point. It opens a packet
// socket on opts.Interface (auto-detect when empty), installs a BPF
// program that admits only "IPv4 + UDP + dstport 161" frames, runs
// for opts.CaptureSeconds, and concurrently classifies each unique
// destination IP via cls.Classify.
//
// Returns nil + the partial result on context cancellation or socket
// errors mid-run; failing-loud is reserved for things the caller
// genuinely cannot recover from (interface doesn't exist, missing
// CAP_NET_RAW).
func CapturePassiveSNMP(ctx context.Context, opts PassiveCaptureOpts, cls SNMPClassifier) ([]PassiveDevice, error) {
	if opts.CaptureSeconds <= 0 {
		opts.CaptureSeconds = 60
	}
	if opts.MaxIPs <= 0 {
		opts.MaxIPs = 4096
	}

	ifaceName := opts.Interface
	if ifaceName == "" {
		n, err := pickDefaultInterface()
		if err != nil {
			return nil, fmt.Errorf("pick interface: %w", err)
		}
		ifaceName = n
	}
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("interface %s: %w", ifaceName, err)
	}

	// AF_PACKET, SOCK_RAW, htons(ETH_P_IP) — see packet(7). The IP
	// EtherType is 0x0800.
	const ethPIP = 0x0800
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(ethPIP)))
	if err != nil {
		if errors.Is(err, unix.EPERM) {
			return nil, fmt.Errorf("AF_PACKET socket: permission denied (need CAP_NET_RAW)")
		}
		return nil, fmt.Errorf("AF_PACKET socket: %w", err)
	}
	defer unix.Close(fd)

	if err := unix.Bind(fd, &unix.SockaddrLinklayer{
		Protocol: htons(ethPIP),
		Ifindex:  iface.Index,
	}); err != nil {
		return nil, fmt.Errorf("bind %s: %w", ifaceName, err)
	}

	// BPF program: admit only IPv4/UDP frames with dst port 161.
	// Compiled offline; matches `tcpdump -d 'udp and dst port 161'`.
	prog, err := bpf.Assemble([]bpf.Instruction{
		// load EtherType (offset 12)
		bpf.LoadAbsolute{Off: 12, Size: 2},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipFalse: 8},
		// IP proto at offset 23
		bpf.LoadAbsolute{Off: 23, Size: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipFalse: 6}, // 17 = UDP
		// fragment offset at 20: skip non-first fragments
		bpf.LoadAbsolute{Off: 20, Size: 2},
		bpf.JumpIf{Cond: bpf.JumpSet, Val: 0x1fff, SkipTrue: 4},
		// IHL into X — 14 (eth) + IP header → load IP[0] & 0x0f << 2
		bpf.LoadMemShift{Off: 14},
		// UDP dst port at IHL + 14 + 2
		bpf.LoadIndirect{Off: 14 + 2, Size: 2},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 161, SkipFalse: 1},
		bpf.RetConstant{Val: 0xffff}, // accept full frame
		bpf.RetConstant{Val: 0},      // drop
	})
	if err != nil {
		return nil, fmt.Errorf("compile BPF: %w", err)
	}
	if err := attachSocketFilter(fd, prog); err != nil {
		// Filter attach failure is non-fatal: we can do the
		// filtering in user space, just less efficiently.
	}

	// 100 ms read timeout so the loop can exit promptly when ctx
	// is cancelled or the capture window elapses.
	tv := unix.Timeval{Sec: 0, Usec: 100_000}
	_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)

	deadline := time.Now().Add(time.Duration(opts.CaptureSeconds) * time.Second)
	buf := make([]byte, 65536)
	dstIPs := make(map[string]struct{}, 256)

capture:
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			break capture
		}
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			// Timeout (EAGAIN) is the normal "no packet this
			// 100ms" path — keep looping.
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			break capture
		}
		if n < 14+20+8 { // eth + min ipv4 + udp
			continue
		}
		// EtherType (offset 12) — the BPF filter already vetted
		// this, but if attachSocketFilter failed we double-check.
		if buf[12] != 0x08 || buf[13] != 0x00 {
			continue
		}
		ihl := int(buf[14]&0x0f) * 4
		if ihl < 20 || 14+ihl+8 > n {
			continue
		}
		if buf[14+9] != 17 { // proto = UDP
			continue
		}
		dstPort := uint16(buf[14+ihl])<<8 | uint16(buf[14+ihl+1])
		if dstPort != 161 {
			continue
		}
		dstIP := net.IPv4(buf[14+16], buf[14+17], buf[14+18], buf[14+19])
		dstIPs[dstIP.String()] = struct{}{}
		if len(dstIPs) >= opts.MaxIPs {
			break capture
		}
	}

	// Classify in parallel — bounded by a small worker pool because
	// each Classify dials SNMP and a 4000-IP capture would otherwise
	// open 4000 sockets.
	ips := make([]string, 0, len(dstIPs))
	for ip := range dstIPs {
		ips = append(ips, ip)
	}

	const classifyWorkers = 16
	jobs := make(chan string, len(ips))
	results := make(chan PassiveDevice, len(ips))
	var wg sync.WaitGroup
	for i := 0; i < classifyWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
				results <- cls.Classify(cctx, ip)
				cancel()
			}
		}()
	}
	for _, ip := range ips {
		jobs <- ip
	}
	close(jobs)
	wg.Wait()
	close(results)

	out := make([]PassiveDevice, 0, len(ips))
	for d := range results {
		out = append(out, d)
	}
	return out, nil
}

// pickDefaultInterface returns the first non-loopback non-down
// interface that has at least one IPv4 address. Good enough for the
// single-NIC collector VMs we deploy on; multi-NIC hosts must set
// the Interface option explicitly.
func pickDefaultInterface() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagLoopback != 0 || ifi.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ip, ok := a.(*net.IPNet); ok && ip.IP.To4() != nil {
				return ifi.Name, nil
			}
		}
	}
	return "", errors.New("no non-loopback IPv4 interface found")
}

// htons converts a host-order uint16 to network byte order. Linux
// AF_PACKET expects the EtherType in network order regardless of CPU
// endianness — we don't have unsafe.Sizeof magic in the stdlib for
// this, so do it explicitly.
func htons(v uint16) uint16 {
	return (v<<8)&0xff00 | v>>8
}

// attachSocketFilter installs a compiled BPF program on a packet
// socket via SO_ATTACH_FILTER. The Go x/net/bpf package gives us the
// classic-BPF instructions; the kernel ABI wants a sock_fprog struct.
func attachSocketFilter(fd int, prog []bpf.RawInstruction) error {
	if len(prog) == 0 {
		return errors.New("empty BPF program")
	}
	// Mirror struct sock_fprog { unsigned short len; struct
	// sock_filter *filter; }. The 6-byte pad is the natural
	// alignment of a pointer following a uint16 on amd64/arm64.
	type sockFprog struct {
		Len    uint16
		_      [6]byte
		Filter *bpf.RawInstruction
	}
	fprog := sockFprog{Len: uint16(len(prog)), Filter: &prog[0]}
	_, _, e := syscall.Syscall6(
		syscall.SYS_SETSOCKOPT,
		uintptr(fd),
		uintptr(unix.SOL_SOCKET),
		uintptr(unix.SO_ATTACH_FILTER),
		uintptr(unsafe.Pointer(&fprog)),
		unsafe.Sizeof(fprog),
		0,
	)
	if e != 0 {
		return fmt.Errorf("SO_ATTACH_FILTER: %w", e)
	}
	return nil
}
