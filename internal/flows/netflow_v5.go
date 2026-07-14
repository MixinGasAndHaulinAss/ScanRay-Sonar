// Package flows parses lightweight NetFlow v5 (and stubs IPFIX) datagrams.
package flows

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

// Record is one decoded flow tuple.
type Record struct {
	SrcAddr net.IP
	DstAddr net.IP
	SrcPort uint16
	DstPort uint16
	Proto   uint8
	Bytes   uint64
	Packets uint64
	First   uint32 // sys uptime ms
	Last    uint32
}

// ParseNetFlowV5 decodes a NetFlow v5 UDP payload. exporter is the
// sender address (stored alongside summaries for troubleshooting).
func ParseNetFlowV5(pkt []byte, exporter net.IP) ([]Record, error) {
	const hdrLen = 24
	const recLen = 48
	if len(pkt) < hdrLen {
		return nil, errors.New("flows: packet too short for v5 header")
	}
	ver := binary.BigEndian.Uint16(pkt[0:2])
	if ver != 5 {
		return nil, fmt.Errorf("flows: expected NetFlow v5, got v%d", ver)
	}
	count := int(binary.BigEndian.Uint16(pkt[2:4]))
	if count <= 0 {
		return nil, nil
	}
	need := hdrLen + count*recLen
	if len(pkt) < need {
		return nil, fmt.Errorf("flows: truncated v5 packet (want %d bytes, have %d)", need, len(pkt))
	}
	out := make([]Record, 0, count)
	off := hdrLen
	for i := 0; i < count; i++ {
		rec := pkt[off : off+recLen]
		off += recLen
		src := net.IPv4(rec[0], rec[1], rec[2], rec[3])
		dst := net.IPv4(rec[4], rec[5], rec[6], rec[7])
		inIf := binary.BigEndian.Uint16(rec[12:14])
		outIf := binary.BigEndian.Uint16(rec[14:16])
		pkts := uint64(binary.BigEndian.Uint32(rec[16:20]))
		octets := uint64(binary.BigEndian.Uint32(rec[20:24]))
		first := binary.BigEndian.Uint32(rec[24:28])
		last := binary.BigEndian.Uint32(rec[28:32])
		srcPort := binary.BigEndian.Uint16(rec[32:34])
		dstPort := binary.BigEndian.Uint16(rec[34:36])
		_ = inIf
		_ = outIf
		proto := rec[38]
		out = append(out, Record{
			SrcAddr: src,
			DstAddr: dst,
			SrcPort: srcPort,
			DstPort: dstPort,
			Proto:   proto,
			Bytes:   octets,
			Packets: pkts,
			First:   first,
			Last:    last,
		})
	}
	_ = exporter
	return out, nil
}

// ParseIPFIX is a best-effort stub: returns nil when the header does not
// look like a minimal IPFIX message we understand yet.
func ParseIPFIX(pkt []byte, exporter net.IP) ([]Record, error) {
	if len(pkt) < 16 {
		return nil, errors.New("flows: IPFIX packet too short")
	}
	ver := binary.BigEndian.Uint16(pkt[0:2])
	if ver != 10 {
		return nil, fmt.Errorf("flows: expected IPFIX v10, got v%d", ver)
	}
	// Full IPFIX template handling is out of scope for M5; callers treat
	// an empty slice as "received but not decoded".
	_ = exporter
	return nil, nil
}

// NowUTC returns the current UTC timestamp for persistence.
func NowUTC() time.Time { return time.Now().UTC() }
