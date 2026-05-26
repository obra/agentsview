package signals

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type scoreGoldenCase struct {
	Name              string         `json:"name"`
	Input             ScoreInput     `json:"input"`
	WantBaselineScore *int           `json:"want_baseline_score"`
	WantScore         *int           `json:"want_score"`
	WantDelta         *int           `json:"want_delta"`
	WantGrade         string         `json:"want_grade"`
	WantBasis         []string       `json:"want_basis"`
	WantPenalties     map[string]int `json:"want_penalties"`
}

func TestComputeHealthScore_GoldenDeltas(t *testing.T) {
	raw, err := os.ReadFile("testdata/score_golden.json")
	if err != nil {
		t.Fatalf("read score golden: %v", err)
	}

	var cases []scoreGoldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse score golden: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := ComputeHealthScore(tc.Input)
			assertIntPtr(t, "Score", got.Score, tc.WantScore)
			if got.Grade != tc.WantGrade {
				t.Fatalf("Grade = %q, want %q", got.Grade, tc.WantGrade)
			}
			if !reflect.DeepEqual(got.Basis, tc.WantBasis) {
				t.Fatalf("Basis = %v, want %v", got.Basis, tc.WantBasis)
			}
			if !reflect.DeepEqual(got.Penalties, tc.WantPenalties) {
				t.Fatalf("Penalties = %v, want %v", got.Penalties, tc.WantPenalties)
			}

			baselineInput := tc.Input
			baselineInput.Heuristics = HeuristicSignals{}
			baseline := ComputeHealthScore(baselineInput)
			assertIntPtr(
				t, "baseline Score",
				baseline.Score, tc.WantBaselineScore,
			)
			assertDelta(t, baseline.Score, got.Score, tc.WantDelta)
		})
	}
}

func assertIntPtr(t *testing.T, name string, got, want *int) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("%s = %d, want %d", name, *got, *want)
	}
}

func assertDelta(t *testing.T, baseline, current, want *int) {
	t.Helper()
	if baseline == nil || current == nil || want == nil {
		if want != nil {
			t.Fatalf(
				"delta unavailable for baseline=%v current=%v, want %d",
				baseline, current, *want,
			)
		}
		return
	}
	got := *current - *baseline
	if got != *want {
		t.Fatalf("delta = %d, want %d", got, *want)
	}
}
