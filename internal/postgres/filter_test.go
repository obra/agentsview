package postgres

import (
	"strings"
	"testing"

	"github.com/wesm/agentsview/internal/db"
)

func TestPGAutomatedScopePredicates(t *testing.T) {
	tests := []struct {
		name     string
		scope    string
		exclude  bool
		want     string
		notWant  string
		buildSQL func(string, bool) string
	}{
		{
			name:    "analytics human",
			scope:   "human",
			want:    "is_automated = FALSE",
			notWant: "is_automated = TRUE",
			buildSQL: func(scope string, exclude bool) string {
				return buildAnalyticsWhereWithDate(
					db.AnalyticsFilter{
						AutomatedScope:   scope,
						ExcludeAutomated: exclude,
					},
					"created_at",
					&paramBuilder{},
					false,
				)
			},
		},
		{
			name:    "analytics all",
			scope:   "all",
			exclude: true,
			notWant: "is_automated = FALSE",
			buildSQL: func(scope string, exclude bool) string {
				return buildAnalyticsWhereWithDate(
					db.AnalyticsFilter{
						AutomatedScope:   scope,
						ExcludeAutomated: exclude,
					},
					"created_at",
					&paramBuilder{},
					false,
				)
			},
		},
		{
			name:    "analytics automated",
			scope:   "automated",
			want:    "is_automated = TRUE",
			notWant: "is_automated = FALSE",
			buildSQL: func(scope string, exclude bool) string {
				return buildAnalyticsWhereWithDate(
					db.AnalyticsFilter{
						AutomatedScope:   scope,
						ExcludeAutomated: exclude,
					},
					"created_at",
					&paramBuilder{},
					false,
				)
			},
		},
		{
			name:  "sessions automated",
			scope: "automated",
			want:  "is_automated = TRUE",
			buildSQL: func(scope string, exclude bool) string {
				sql, _ := buildPGSessionFilter(db.SessionFilter{
					AutomatedScope:   scope,
					ExcludeAutomated: exclude,
				})
				return sql
			},
		},
		{
			name:  "usage automated",
			scope: "automated",
			want:  "COALESCE(u.is_automated, false) = TRUE",
			buildSQL: func(scope string, exclude bool) string {
				sql, _ := appendPGUsageRowFilterClauses(
					"WHERE true",
					&paramBuilder{},
					db.UsageFilter{
						AutomatedScope:   scope,
						ExcludeAutomated: exclude,
					},
				)
				return sql
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql := tt.buildSQL(tt.scope, tt.exclude)
			if tt.want != "" && !strings.Contains(sql, tt.want) {
				t.Fatalf("SQL missing %q: %s", tt.want, sql)
			}
			if tt.notWant != "" && strings.Contains(sql, tt.notWant) {
				t.Fatalf("SQL unexpectedly contains %q: %s", tt.notWant, sql)
			}
		})
	}
}

func TestPGAutomatedScopeOneShotExemption(t *testing.T) {
	sql := buildAnalyticsWhereWithDate(
		db.AnalyticsFilter{
			AutomatedScope: "automated",
			ExcludeOneShot: true,
		},
		"created_at",
		&paramBuilder{},
		false,
	)
	want := "(user_message_count > 1 OR is_automated = TRUE)"
	if !strings.Contains(sql, want) {
		t.Fatalf("analytics SQL missing one-shot exemption %q: %s", want, sql)
	}

	usageSQL, _ := appendPGUsageRowFilterClauses(
		"WHERE true",
		&paramBuilder{},
		db.UsageFilter{
			AutomatedScope: "automated",
			ExcludeOneShot: true,
		},
	)
	want = "(u.user_message_count > 1 OR COALESCE(u.is_automated, false) = TRUE)"
	if !strings.Contains(usageSQL, want) {
		t.Fatalf("usage SQL missing one-shot exemption %q: %s", want, usageSQL)
	}
}
