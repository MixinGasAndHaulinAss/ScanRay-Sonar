package api

import (
	"math"
	"testing"
)

// floatPtr / intPtr — tiny helpers so the table below stays readable.
func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

func TestComputeScore_PerfectScoreOnEmptyInputs(t *testing.T) {
	got := ComputeScore(ScoreInputs{})
	if got != 10.0 {
		t.Fatalf("empty inputs should score 10.0, got %.2f", got)
	}
}

func TestComputeScore_FloorIsZero(t *testing.T) {
	in := ScoreInputs{
		CPUPct:           floatPtr(100),
		MemPct:           floatPtr(100),
		DiskPct:          floatPtr(100),
		BatteryHealthPct: floatPtr(0),
		BSODCount24h:     intPtr(5),
		AppCrashCount24h: intPtr(500),
		LatencyAvgMs:     floatPtr(1000),
		LossPct:          floatPtr(100),
	}
	got := ComputeScore(in)
	if got < 0 || got > 0.0001 {
		t.Fatalf("worst-case inputs should clamp to 0, got %.2f", got)
	}
}

func TestComputeScore_BSODsAreBinary(t *testing.T) {
	// One BSOD penalises 2 points; 50 BSODs also penalise 2 points
	// (the formula is binary, not graduated). This pins that
	// behaviour so a future change is deliberate.
	one := ComputeScore(ScoreInputs{BSODCount24h: intPtr(1)})
	many := ComputeScore(ScoreInputs{BSODCount24h: intPtr(50)})
	if math.Abs(one-many) > 0.0001 {
		t.Fatalf("BSOD penalty should be flat: 1=%v vs 50=%v", one, many)
	}
	if math.Abs(10-one-2.0) > 0.0001 {
		t.Fatalf("BSOD penalty should be 2 points, got %.2f", 10-one)
	}
}

func TestComputeScore_LinearCPUMemoryDisk(t *testing.T) {
	cases := []struct {
		name string
		in   ScoreInputs
		want float64
	}{
		{"cpu_50pct_no_penalty", ScoreInputs{CPUPct: floatPtr(50)}, 10.0},
		{"cpu_75pct_half_penalty", ScoreInputs{CPUPct: floatPtr(75)}, 9.0},
		{"cpu_100pct_full_penalty", ScoreInputs{CPUPct: floatPtr(100)}, 8.0},
		{"mem_60_no_penalty", ScoreInputs{MemPct: floatPtr(60)}, 10.0},
		{"mem_100_full_penalty", ScoreInputs{MemPct: floatPtr(100)}, 8.5},
		{"disk_70_no_penalty", ScoreInputs{DiskPct: floatPtr(70)}, 10.0},
		{"disk_100_full_penalty", ScoreInputs{DiskPct: floatPtr(100)}, 8.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeScore(tc.in)
			if math.Abs(got-tc.want) > 0.05 {
				t.Fatalf("got %.2f, want %.2f", got, tc.want)
			}
		})
	}
}

func TestComputeScore_BatteryInverted(t *testing.T) {
	// 70% battery health → no penalty.
	if got := ComputeScore(ScoreInputs{BatteryHealthPct: floatPtr(70)}); math.Abs(got-10) > 0.05 {
		t.Fatalf("battery 70%% should be no penalty, got %.2f", got)
	}
	// 30% battery health → full 1-point penalty.
	if got := ComputeScore(ScoreInputs{BatteryHealthPct: floatPtr(30)}); math.Abs(got-9) > 0.05 {
		t.Fatalf("battery 30%% should be -1 point, got %.2f", got)
	}
	// 100% battery health → still no penalty (clamps at 70).
	if got := ComputeScore(ScoreInputs{BatteryHealthPct: floatPtr(100)}); math.Abs(got-10) > 0.05 {
		t.Fatalf("battery 100%% should be no penalty, got %.2f", got)
	}
}

func TestComputeScore_LatencyAndLossStack(t *testing.T) {
	in := ScoreInputs{
		LatencyAvgMs: floatPtr(150), // halfway through 50→250 → 0.5 of 1.5 = 0.75
		LossPct:      floatPtr(10),  // 10/25 of 1 → 0.4
	}
	want := 10.0 - 0.5*1.5 - 0.4
	got := ComputeScore(in)
	if math.Abs(got-math.Round(want*10)/10) > 0.06 {
		t.Fatalf("got %.2f, want ~%.2f", got, want)
	}
}

func TestComputeScore_RoundsToOneDecimal(t *testing.T) {
	got := ComputeScore(ScoreInputs{CPUPct: floatPtr(73.333)})
	// Validate the result has at most one decimal of precision.
	scaled := got * 10
	if math.Abs(scaled-math.Round(scaled)) > 1e-9 {
		t.Fatalf("score should be rounded to 1 decimal, got %v", got)
	}
}
