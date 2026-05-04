// score.go — device "experience" score formula. One source of truth
// shared by the Devices-Performance, Devices-Averages, and
// User-Experience overview endpoints so the three never disagree
// about whether a host is healthy.
//
// The score is on 0–10 to match the gauges in the dashboards
// (screenshots show "Average Score" rendered out of 10). Each input
// has a documented penalty band so operators can predict why a
// specific host scored low without reading code.

package api

import "math"

// ScoreInputs are the per-host signals the formula consumes. All
// fields are nullable: the formula simply skips a missing input
// (its penalty contributes 0). That keeps a freshly-enrolled host
// without a snapshot from scoring 0/10 just because it hasn't
// reported anything yet.
type ScoreInputs struct {
	CPUPct           *float64
	MemPct           *float64
	DiskPct          *float64
	BatteryHealthPct *float64
	BSODCount24h     *int
	AppCrashCount24h *int
	LatencyAvgMs     *float64
	LossPct          *float64
}

// ComputeScore returns a 0..10 score (1 decimal). Empty/nil inputs
// drop their penalties — the maximum achievable score is always 10
// regardless of which signals are present.
//
// Penalty bands (each clamps to the [0, max] interval):
//
//	CPU%       linear, 50 → 0 pts, 100 → 2 pts                (max 2.0)
//	Memory%    linear, 60 → 0 pts, 100 → 1.5 pts              (max 1.5)
//	Disk%      linear, 70 → 0 pts, 100 → 1.5 pts              (max 1.5)
//	Battery%   linear, 70 → 0 pts, 30 → 1 pt                  (max 1.0)
//	BSODs      0 → 0 pts, 1+ → 2 pts                          (max 2.0)
//	AppCrash   linear, 0 → 0 pts, 50+ → 1 pt                  (max 1.0)
//	LatencyMs  linear, 50ms → 0 pts, 250ms → 1.5 pts          (max 1.5)
//	Loss%      linear, 0 → 0 pts, 25% → 1 pt                  (max 1.0)
func ComputeScore(in ScoreInputs) float64 {
	score := 10.0

	if in.CPUPct != nil {
		score -= scaledPenalty(*in.CPUPct, 50, 100, 2.0)
	}
	if in.MemPct != nil {
		score -= scaledPenalty(*in.MemPct, 60, 100, 1.5)
	}
	if in.DiskPct != nil {
		score -= scaledPenalty(*in.DiskPct, 70, 100, 1.5)
	}
	if in.BatteryHealthPct != nil {
		// Inverted: battery health "drops" as percentage falls.
		score -= scaledPenalty(70-*in.BatteryHealthPct, 0, 40, 1.0)
	}
	if in.BSODCount24h != nil && *in.BSODCount24h > 0 {
		score -= 2.0
	}
	if in.AppCrashCount24h != nil {
		score -= scaledPenalty(float64(*in.AppCrashCount24h), 0, 50, 1.0)
	}
	if in.LatencyAvgMs != nil {
		score -= scaledPenalty(*in.LatencyAvgMs, 50, 250, 1.5)
	}
	if in.LossPct != nil {
		score -= scaledPenalty(*in.LossPct, 0, 25, 1.0)
	}

	if score < 0 {
		score = 0
	}
	if score > 10 {
		score = 10
	}
	// Round to one decimal so wire payloads compare cleanly.
	return math.Round(score*10) / 10
}

// scaledPenalty linearly interpolates between (begin, 0) and (end,
// maxPenalty), clamped at both ends. begin > end is allowed (used
// for the inverted battery-health calculation).
func scaledPenalty(v, begin, end, maxPenalty float64) float64 {
	if end == begin {
		if v >= begin {
			return maxPenalty
		}
		return 0
	}
	t := (v - begin) / (end - begin)
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return maxPenalty
	}
	return t * maxPenalty
}
