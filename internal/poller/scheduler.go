// Package poller is the orchestration layer between Postgres
// (appliances table → poll targets) and internal/snmp (collectors).
//
// Design goals:
//   - One goroutine per appliance, ticking at its own poll_interval_s.
//   - Concurrency cap so a 1000-port site doesn't open 1000 SNMP
//     sessions at once.
//   - Hot reload — the scheduler periodically re-reads the appliances
//     table; new rows pick up without a restart, deleted rows shut
//     down their workers.
//   - State on rate calculation lives in-memory keyed by
//     (appliance_id, if_index). Counter wraps and "first poll" cases
//     are handled cleanly so the UI never sees a sentinel zero or a
//     nonsense "1 Tbps" spike.
package poller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	scrypto "github.com/NCLGISA/ScanRay-Sonar/internal/crypto"
	"github.com/NCLGISA/ScanRay-Sonar/internal/snmp"
)

// MaxParallelPolls caps how many appliances can be mid-poll at the
// same time. Each in-flight poll holds open one UDP socket and
// roughly 50–500 KiB of result buffers, so 50 concurrent gives us
// plenty of headroom on a 1 vCPU / 512 MiB poller.
const MaxParallelPolls = 50

// reloadInterval is how often we re-read the appliances table to pick
// up new rows / drop deleted ones. 30s is a fair compromise between
// "operator clicked add and it polled within a minute" and "don't
// hammer postgres".
const reloadInterval = 30 * time.Second

// Scheduler is the top-level loop. Construct with New, call Run; it
// blocks until ctx is cancelled.
type Scheduler struct {
	pool   *pgxpool.Pool
	sealer *scrypto.Sealer
	log    *slog.Logger

	// sem caps concurrent SNMP sessions across all worker goroutines.
	sem chan struct{}

	// rate state: last raw counter readings per (appliance, ifIndex),
	// used to compute bps. Modulus-arithmetic friendly so a Counter64
	// wrap looks like a small positive delta, not a 18-exa-byte spike.
	rateMu sync.Mutex
	rate   map[rateKey]rateState

	// active workers, one per appliance. Cancelling the func stops
	// the worker after its current poll completes.
	workersMu sync.Mutex
	workers   map[uuid.UUID]workerHandle
}

type workerHandle struct {
	cancel       context.CancelFunc
	pollInterval time.Duration
}

type rateKey struct {
	apID uuid.UUID
	idx  int32
}
type rateState struct {
	t           time.Time
	inOctets    uint64
	outOctets   uint64
	inErrors    uint64
	outErrors   uint64
	inDiscards  uint64
	outDiscards uint64
}

// New constructs a Scheduler. The sealer is needed to decrypt
// per-appliance SNMP credentials from the appliances row.
func New(pool *pgxpool.Pool, sealer *scrypto.Sealer, log *slog.Logger) *Scheduler {
	return &Scheduler{
		pool:    pool,
		sealer:  sealer,
		log:     log,
		sem:     make(chan struct{}, MaxParallelPolls),
		rate:    map[rateKey]rateState{},
		workers: map[uuid.UUID]workerHandle{},
	}
}

// Run blocks until ctx is cancelled. It periodically reconciles the
// set of running per-appliance workers with the appliances table.
func (s *Scheduler) Run(ctx context.Context) {
	s.log.Info("poller scheduler starting", "max_parallel", MaxParallelPolls, "reload_s", int(reloadInterval.Seconds()))

	t := time.NewTicker(reloadInterval)
	defer t.Stop()

	s.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			s.log.Info("poller scheduler shutting down; stopping workers")
			s.stopAllWorkers()
			return
		case <-t.C:
			s.reconcile(ctx)
		}
	}
}

// reconcile is the table → workers reconciliation step. New active
// rows get a worker, deleted/inactive rows stop theirs, and an
// interval change tears down + restarts the worker so the new tick
// rate takes effect immediately.
func (s *Scheduler) reconcile(ctx context.Context) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, host(mgmt_ip), snmp_version, enc_snmp_creds, poll_interval_s, vendor
		  FROM appliances
		 WHERE is_active = TRUE
		   AND enc_snmp_creds IS NOT NULL
		   AND collector_id IS NULL
	`)
	if err != nil {
		s.log.Warn("scheduler: list appliances failed", "err", err)
		return
	}
	defer rows.Close()

	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var (
			id      uuid.UUID
			ip      string
			snmpv   string
			sealed  []byte
			pollSec int
			vendor  string
		)
		if err := rows.Scan(&id, &ip, &snmpv, &sealed, &pollSec, &vendor); err != nil {
			s.log.Warn("scheduler: scan failed", "err", err)
			continue
		}
		seen[id] = true
		interval := time.Duration(pollSec) * time.Second
		if interval < 15*time.Second {
			interval = 15 * time.Second
		}

		s.workersMu.Lock()
		existing, ok := s.workers[id]
		s.workersMu.Unlock()

		if ok && existing.pollInterval == interval {
			continue // already running with the right cadence
		}
		if ok {
			existing.cancel() // interval changed → restart
		}

		s.startWorker(ctx, id, ip, snmpv, sealed, vendor, interval)
	}

	// Stop workers whose appliances vanished (deleted or deactivated).
	s.workersMu.Lock()
	for id, h := range s.workers {
		if !seen[id] {
			h.cancel()
			delete(s.workers, id)
		}
	}
	s.workersMu.Unlock()
}

func (s *Scheduler) startWorker(parent context.Context, id uuid.UUID, ip, snmpv string, sealed []byte, vendor string, interval time.Duration) {
	ctx, cancel := context.WithCancel(parent)
	s.workersMu.Lock()
	s.workers[id] = workerHandle{cancel: cancel, pollInterval: interval}
	s.workersMu.Unlock()

	creds, err := s.openCreds(id, sealed)
	if err != nil {
		s.log.Warn("scheduler: open creds failed; worker will retry on next reconcile",
			"appliance_id", id, "err", err)
		s.recordPollError(parent, id, fmt.Errorf("decrypt creds: %w", err))
		cancel()
		return
	}

	go s.workerLoop(ctx, id, ip, vendor, interval, creds)
}

// workerLoop is one appliance's per-tick poll loop. The first tick
// fires immediately so a freshly-added appliance shows data quickly,
// then settles into the configured cadence.
func (s *Scheduler) workerLoop(ctx context.Context, id uuid.UUID, ip, vendor string, interval time.Duration, creds snmp.Creds) {
	s.log.Info("poller worker started", "appliance_id", id, "ip", ip, "interval_s", int(interval.Seconds()), "vendor", vendor)

	// First poll right away.
	s.pollOnce(ctx, id, ip, vendor, creds)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.log.Info("poller worker stopped", "appliance_id", id)
			return
		case <-t.C:
			s.pollOnce(ctx, id, ip, vendor, creds)
		}
	}
}

// pollOnce dials, collects, computes rates, persists, then closes.
// All errors are logged + written to appliances.last_error so an
// operator can see the failure in the UI without tailing the poller
// container's stdout.
func (s *Scheduler) pollOnce(ctx context.Context, id uuid.UUID, ip, vendor string, creds snmp.Creds) {
	// Bound the per-poll lifetime so a black-holed device doesn't
	// hold a worker forever (and a slot in the semaphore).
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	select {
	case s.sem <- struct{}{}:
	case <-pctx.Done():
		return
	}
	defer func() { <-s.sem }()

	if net.ParseIP(ip) == nil {
		s.recordPollError(ctx, id, fmt.Errorf("invalid mgmt_ip %q", ip))
		return
	}

	c, err := snmp.Dial(pctx, snmp.Target{Host: ip}, creds)
	if err != nil {
		s.recordPollError(ctx, id, fmt.Errorf("dial: %w", err))
		return
	}
	defer c.Close()

	snap := snmp.CollectAll(pctx, c)

	switch vendor {
	case "cisco":
		snap.Chassis = snmp.CollectCiscoChassis(pctx, c)
	}

	s.computeRates(id, &snap)

	if err := s.persist(ctx, id, snap); err != nil {
		s.recordPollError(ctx, id, fmt.Errorf("persist: %w", err))
		return
	}
}

// computeRates fills Interface.InBps/OutBps based on previous-poll
// readings. The first time we see an (appliance, ifIndex) pair we
// store the reading and return without setting *Bps — the UI will
// render "—" for that row until the second poll lands.
//
// Counter64 wraps are handled by computing the delta in modular
// arithmetic on uint64 (`cur - prev` naturally rolls over). A
// counter discontinuity (device reboot) is detected by uptime drop
// at a higher level; here we just clamp implausibly large rates
// (>10 Tbps) so a missed wrap doesn't pollute the dashboard.
func (s *Scheduler) computeRates(id uuid.UUID, snap *snmp.Snapshot) {
	now := snap.CapturedAt

	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	for i := range snap.Interfaces {
		ifc := &snap.Interfaces[i]
		k := rateKey{apID: id, idx: ifc.Index}
		prev, hadPrev := s.rate[k]

		// Always update the stored sample for next time.
		s.rate[k] = rateState{
			t:           now,
			inOctets:    ifc.InOctets,
			outOctets:   ifc.OutOctets,
			inErrors:    ifc.InErrors,
			outErrors:   ifc.OutErrors,
			inDiscards:  ifc.InDiscards,
			outDiscards: ifc.OutDiscards,
		}

		if !hadPrev || prev.t.IsZero() {
			continue
		}
		dt := now.Sub(prev.t).Seconds()
		if dt <= 0 {
			continue
		}
		inBps := bpsDelta(prev.inOctets, ifc.InOctets, dt)
		outBps := bpsDelta(prev.outOctets, ifc.OutOctets, dt)
		if inBps != nil {
			ifc.InBps = inBps
		}
		if outBps != nil {
			ifc.OutBps = outBps
		}
	}
}

// bpsDelta returns nil when the delta is implausible (>10 Tbps),
// which we treat as a missed counter wrap rather than a real spike.
func bpsDelta(prev, cur uint64, dtSec float64) *uint64 {
	delta := cur - prev // uint64 modular arithmetic handles a single wrap
	bps := uint64(float64(delta) * 8 / dtSec)
	const sanityMaxBps = 10_000_000_000_000 // 10 Tbps
	if bps > sanityMaxBps {
		return nil
	}
	return &bps
}

// persist updates appliances.last_snapshot + denormalized columns and
// inserts time-series samples for chassis + per-port. All writes share
// a transaction so a dashboard reading mid-poll never sees the
// snapshot updated but samples missing.
func (s *Scheduler) persist(ctx context.Context, id uuid.UUID, snap snmp.Snapshot) error {
	return PersistSnapshot(ctx, s.pool, id, snap)
}

func (s *Scheduler) recordPollError(ctx context.Context, id uuid.UUID, pollErr error) {
	RecordPollError(ctx, s.pool, s.log, id, pollErr)
}

func (s *Scheduler) openCreds(id uuid.UUID, sealed []byte) (snmp.Creds, error) {
	if len(sealed) == 0 {
		return snmp.Creds{}, errors.New("no sealed creds on row")
	}
	plain, err := s.sealer.Open(sealed, []byte("appliance:"+id.String()))
	if err != nil {
		return snmp.Creds{}, err
	}
	var c snmp.Creds
	if err := json.Unmarshal(plain, &c); err != nil {
		return snmp.Creds{}, err
	}
	return c, nil
}

func (s *Scheduler) stopAllWorkers() {
	s.workersMu.Lock()
	defer s.workersMu.Unlock()
	for id, h := range s.workers {
		h.cancel()
		delete(s.workers, id)
	}
}
