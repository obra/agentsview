package duckdb

import (
	"strings"
	"testing"

	"go.kenn.io/agentsview/internal/db"
)

func TestDuckAnalyticsAutomatedScopePredicates(t *testing.T) {
	tests := []struct {
		name    string
		filter  db.AnalyticsFilter
		want    string
		notWant string
	}{
		{
			name: "legacy exclude automated normalizes to human",
			filter: db.AnalyticsFilter{
				ExcludeAutomated: true,
			},
			want: "s.is_automated = FALSE",
		},
		{
			name: "all scope suppresses legacy human filter",
			filter: db.AnalyticsFilter{
				AutomatedScope:   "all",
				ExcludeAutomated: true,
			},
			notWant: "s.is_automated = FALSE",
		},
		{
			name: "automated scope selects automated sessions",
			filter: db.AnalyticsFilter{
				AutomatedScope: "automated",
			},
			want:    "s.is_automated = TRUE",
			notWant: "s.is_automated = FALSE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _ := duckBuildAnalyticsWhere(
				tt.filter,
				"COALESCE(s.started_at, s.created_at)",
				"s.",
				false,
				false,
			)
			if tt.want != "" && !strings.Contains(sql, tt.want) {
				t.Fatalf("DuckDB analytics SQL missing %q: %s", tt.want, sql)
			}
			if tt.notWant != "" && strings.Contains(sql, tt.notWant) {
				t.Fatalf("DuckDB analytics SQL unexpectedly contains %q: %s", tt.notWant, sql)
			}
		})
	}
}

func TestDuckAnalyticsAutomatedScopeOneShotExemption(t *testing.T) {
	sql, _ := duckBuildAnalyticsWhere(
		db.AnalyticsFilter{
			AutomatedScope: "automated",
			ExcludeOneShot: true,
		},
		"COALESCE(s.started_at, s.created_at)",
		"s.",
		false,
		false,
	)
	want := "(s.user_message_count > 1 OR s.is_automated = TRUE)"
	if !strings.Contains(sql, want) {
		t.Fatalf("DuckDB analytics SQL missing one-shot exemption %q: %s", want, sql)
	}
}
