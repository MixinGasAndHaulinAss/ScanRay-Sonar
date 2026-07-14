package flows

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Listener receives NetFlow/IPFIX datagrams and persists summaries.
type Listener struct {
	addr string
	pool *pgxpool.Pool
	log  *slog.Logger

	mu      sync.Mutex
	pending []persistRow
}

type persistRow struct {
	t         time.Time
	src, dst  net.IP
	srcPort   int
	dstPort   int
	proto     int
	bytes     int64
	packets   int64
	exporter  net.IP
}

// NewListener binds UDP addr (e.g. ":2055").
func NewListener(addr string, pool *pgxpool.Pool, log *slog.Logger) *Listener {
	return &Listener{addr: addr, pool: pool, log: log}
}

// Run blocks until ctx is cancelled.
func (l *Listener) Run(ctx context.Context) error {
	pc, err := net.ListenPacket("udp", l.addr)
	if err != nil {
		return err
	}
	defer pc.Close()
	l.log.Info("flow listener started", "addr", l.addr)

	flushTicker := time.NewTicker(5 * time.Second)
	defer flushTicker.Stop()

	buf := make([]byte, 65535)
	for {
		select {
		case <-ctx.Done():
			l.flush(ctx)
			return ctx.Err()
		case <-flushTicker.C:
			l.flush(ctx)
		default:
			_ = pc.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				if ctx.Err() != nil {
					l.flush(ctx)
					return ctx.Err()
				}
				l.log.Debug("flow read failed", "err", err)
				continue
			}
			var exporter net.IP
			if ua, ok := addr.(*net.UDPAddr); ok {
				exporter = ua.IP
			}
			l.handlePacket(buf[:n], exporter)
		}
	}
}

func (l *Listener) handlePacket(pkt []byte, exporter net.IP) {
	var recs []Record
	var err error
	if len(pkt) >= 2 && binaryVersion(pkt) == 5 {
		recs, err = ParseNetFlowV5(pkt, exporter)
	} else if len(pkt) >= 2 && binaryVersion(pkt) == 10 {
		recs, err = ParseIPFIX(pkt, exporter)
	} else {
		return
	}
	if err != nil {
		l.log.Debug("flow parse failed", "err", err, "exporter", exporter)
		return
	}
	if len(recs) == 0 {
		return
	}
	now := NowUTC()
	l.mu.Lock()
	for _, r := range recs {
		l.pending = append(l.pending, persistRow{
			t: now, src: r.SrcAddr, dst: r.DstAddr,
			srcPort: int(r.SrcPort), dstPort: int(r.DstPort),
			proto: int(r.Proto), bytes: int64(r.Bytes), packets: int64(r.Packets),
			exporter: exporter,
		})
	}
	shouldFlush := len(l.pending) >= 500
	l.mu.Unlock()
	if shouldFlush {
		l.flush(context.Background())
	}
}

func binaryVersion(pkt []byte) uint16 {
	if len(pkt) < 2 {
		return 0
	}
	return uint16(pkt[0])<<8 | uint16(pkt[1])
}

func (l *Listener) flush(ctx context.Context) {
	l.mu.Lock()
	batch := l.pending
	l.pending = nil
	l.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for _, row := range batch {
		_, err := l.pool.Exec(fctx, `
			INSERT INTO flow_summaries (time, src_addr, dst_addr, src_port, dst_port, proto, bytes, packets, exporter_ip)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			row.t, row.src.String(), row.dst.String(),
			nullInt(row.srcPort), nullInt(row.dstPort),
			row.proto, row.bytes, row.packets, nullableIP(row.exporter))
		if err != nil {
			l.log.Warn("flow insert failed", "err", err)
			return
		}
	}
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullableIP(ip net.IP) any {
	if ip == nil || len(ip) == 0 {
		return nil
	}
	return ip.String()
}
