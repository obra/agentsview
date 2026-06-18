package duckdb

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestDuckUsageTerminationPredicate(t *testing.T) {
	where, args := appendDuckUsageSessionFilterClauses(
		"WHERE true",
		nil,
		db.UsageFilter{Termination: "clean,unclean"},
		"",
	)

	assert.Contains(t, where, "s.termination_status = 'clean'")
	assert.Contains(t, where, "s.termination_status IN ('tool_call_pending', 'truncated')")
	assert.Contains(t, where, "COALESCE(s.ended_at, s.started_at, s.created_at) <= CAST(? AS TIMESTAMP)")
	require.Len(t, args, 1)
	_, ok := args[0].(string)
	assert.True(t, ok, "termination cutoff should be bound as a timestamp string")
}

func TestDuckSignalMessagesFormatsTimestampValues(t *testing.T) {
	ctx := context.Background()
	store, _ := newSyncedStore(t)

	_, err := store.duck.ExecContext(ctx, `
		INSERT INTO messages (
			id, session_id, ordinal, role, content, timestamp,
			is_system, has_tool_use
		) VALUES
			(9101, 'signal-time', 0, 'user', 'with timestamp',
			 CAST('2026-01-20T12:34:56Z' AS TIMESTAMP), FALSE, FALSE),
			(9102, 'signal-time', 1, 'assistant', 'without timestamp',
			 NULL, FALSE, FALSE)`)
	require.NoError(t, err)

	got, err := store.duckSignalMessages(ctx, []db.SignalRow{{ID: "signal-time"}})
	require.NoError(t, err)
	require.Len(t, got["signal-time"], 2)
	assert.Equal(t, "2026-01-20T12:34:56Z", got["signal-time"][0].Timestamp)
	assert.Empty(t, got["signal-time"][1].Timestamp)
}
