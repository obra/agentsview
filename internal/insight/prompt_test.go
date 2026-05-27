package insight

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/agentsview/internal/db"
	"go.kenn.io/agentsview/internal/dbtest"
)

func TestBuildPrompt(t *testing.T) {
	tests := []struct {
		name         string
		req          GenerateRequest
		seed         func(t *testing.T, d *db.DB)
		wantContains []string
		wantNot      []string
		checkPrompt  func(t *testing.T, prompt string)
	}{
		{
			name: "with sessions",
			req: GenerateRequest{
				Type:     "daily_activity",
				DateFrom: "2025-01-15",
				DateTo:   "2025-01-15",
			},
			seed: func(t *testing.T, d *db.DB) {
				dbtest.SeedSession(t, d, "s1", "my-app", func(s *db.Session) {
					s.MessageCount = 5
					s.StartedAt = new("2025-01-15T10:00:00Z")
					s.EndedAt = new("2025-01-15T11:00:00Z")
					s.FirstMessage = new("Fix the login bug")
				})
				dbtest.SeedSession(t, d, "s2", "other-app", func(s *db.Session) {
					s.MessageCount = 3
					s.StartedAt = new("2025-01-15T14:00:00Z")
					s.EndedAt = new("2025-01-15T15:00:00Z")
					s.FirstMessage = new("Add tests")
				})
			},
			wantContains: []string{
				"summarizing a day",
				"Date: 2025-01-15",
				"s1",
				"my-app",
				"Fix the login bug",
				"s2",
				"other-app",
				"Add tests",
			},
		},
		{
			name: "project filter",
			req: GenerateRequest{
				Type:     "daily_activity",
				DateFrom: "2025-01-15",
				DateTo:   "2025-01-15",
				Project:  "my-app",
			},
			seed: func(t *testing.T, d *db.DB) {
				dbtest.SeedSession(t, d, "s1", "my-app", func(s *db.Session) {
					s.MessageCount = 5
					s.StartedAt = new("2025-01-15T10:00:00Z")
					s.EndedAt = new("2025-01-15T11:00:00Z")
				})
				dbtest.SeedSession(t, d, "s2", "other-app", func(s *db.Session) {
					s.MessageCount = 3
					s.StartedAt = new("2025-01-15T14:00:00Z")
					s.EndedAt = new("2025-01-15T15:00:00Z")
				})
			},
			wantContains: []string{"Project: my-app"},
			wantNot:      []string{"other-app"},
		},
		{
			name: "user prompt",
			req: GenerateRequest{
				Type:     "daily_activity",
				DateFrom: "2025-01-15",
				DateTo:   "2025-01-15",
				Prompt:   "Focus on security improvements",
			},
			wantContains: []string{
				"User Query",
				"Prioritize addressing",
				"Focus on security improvements",
			},
		},
		{
			name: "agent analysis",
			req: GenerateRequest{
				Type:     "agent_analysis",
				DateFrom: "2025-01-15",
				DateTo:   "2025-01-15",
			},
			wantContains: []string{"analyzing AI agent"},
		},
		{
			name: "truncation",
			req: GenerateRequest{
				Type:     "daily_activity",
				DateFrom: "2025-01-15",
				DateTo:   "2025-01-15",
			},
			seed: func(t *testing.T, d *db.DB) {
				for i := range 55 {
					dbtest.SeedSession(
						t, d,
						fmt.Sprintf("s%d", i), "my-app",
						func(s *db.Session) {
							s.MessageCount = 1
							s.StartedAt = new("2025-01-15T10:00:00Z")
							s.EndedAt = new(fmt.Sprintf("2025-01-15T11:%02d:00Z", i))
						},
					)
				}
			},
			wantContains: []string{"omitted"},
			checkPrompt: func(t *testing.T, prompt string) {
				count := strings.Count(prompt, "### Session")
				assert.Equal(t, 50, count, "got %d sessions in prompt, want 50", count)
			},
		},
		{
			name: "date range",
			req: GenerateRequest{
				Type:     "daily_activity",
				DateFrom: "2025-01-13",
				DateTo:   "2025-01-17",
			},
			seed: func(t *testing.T, d *db.DB) {
				dbtest.SeedSession(t, d, "s1", "my-app", func(s *db.Session) {
					s.MessageCount = 3
					s.StartedAt = new("2025-01-13T10:00:00Z")
				})
				dbtest.SeedSession(t, d, "s2", "my-app", func(s *db.Session) {
					s.MessageCount = 2
					s.StartedAt = new("2025-01-17T14:00:00Z")
				})
			},
			wantContains: []string{"Date Range: 2025-01-13 to 2025-01-17"},
			wantNot:      []string{"Date: 2025"},
		},
		{
			name: "date range no sessions",
			req: GenerateRequest{
				Type:     "daily_activity",
				DateFrom: "2025-01-13",
				DateTo:   "2025-01-17",
			},
			wantContains: []string{"date range"},
		},
		{
			name: "no sessions",
			req: GenerateRequest{
				Type:     "daily_activity",
				DateFrom: "2025-01-15",
				DateTo:   "2025-01-15",
			},
			wantContains: []string{"No sessions found"},
		},
		{
			name: "excludes automated sessions",
			req: GenerateRequest{
				Type:     "daily_activity",
				DateFrom: "2025-01-15",
				DateTo:   "2025-01-15",
			},
			seed: func(t *testing.T, d *db.DB) {
				// A normal user session.
				dbtest.SeedSession(t, d, "user-session", "my-app", func(s *db.Session) {
					s.MessageCount = 5
					s.UserMessageCount = 2
					s.StartedAt = new("2025-01-15T10:00:00Z")
					s.EndedAt = new("2025-01-15T11:00:00Z")
					s.FirstMessage = new("Fix the login bug")
				})
				// An automated session: roborev review, single-turn,
				// is_automated must be true.
				dbtest.SeedSession(t, d, "auto-session", "my-app", func(s *db.Session) {
					s.MessageCount = 2
					s.UserMessageCount = 1
					s.StartedAt = new("2025-01-15T12:00:00Z")
					s.EndedAt = new("2025-01-15T12:05:00Z")
					s.FirstMessage = new(
						"You are a code reviewer. Review the diff.",
					)
					s.IsAutomated = true
				})
			},
			wantContains: []string{"user-session", "Fix the login bug"},
			wantNot:      []string{"auto-session", "code reviewer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := dbtest.OpenTestDB(t)
			ctx := context.Background()

			if tt.seed != nil {
				tt.seed(t, d)
			}

			prompt, err := BuildPrompt(ctx, d, tt.req)
			require.NoError(t, err)

			for _, want := range tt.wantContains {
				assert.Contains(t, prompt, want)
			}
			for _, notWant := range tt.wantNot {
				assert.NotContains(t, prompt, notWant)
			}
			if tt.checkPrompt != nil {
				tt.checkPrompt(t, prompt)
			}
		})
	}
}

func TestCannedInsightValidation(t *testing.T) {
	payload := CannedAggregatePayload{
		Kind:     CannedPromptMaturityReview,
		DateFrom: "2025-01-15",
		DateTo:   "2025-01-15",
		EvidenceRefs: []CannedEvidenceRef{
			{ID: "signals:score_distribution", Description: "scores"},
		},
	}
	valid := `{
		"schema_version":"llm_insight.v1",
		"kind":"prompt_maturity_review",
		"summary":"Tighten prompt starts.",
		"confidence":"medium",
		"recommendations":[{
			"title":"Add acceptance criteria",
			"rationale":"The score distribution supports reviewing prompt starts.",
			"actions":["Add a done definition"],
			"evidence_refs":["signals:score_distribution"],
			"impact":"medium",
			"effort":"low"
		}],
		"risks":[],
		"evidence_refs":["signals:score_distribution"]
	}`

	env, err := ParseCannedEnvelope(valid)
	if err != nil {
		t.Fatalf("ParseCannedEnvelope: %v", err)
	}
	if err := ValidateCannedEnvelope(env, payload); err != nil {
		t.Fatalf("ValidateCannedEnvelope: %v", err)
	}

	bad := strings.Replace(
		valid,
		`"signals:score_distribution"`,
		`"signals:not_real"`,
		1,
	)
	env, err = ParseCannedEnvelope(bad)
	if err != nil {
		t.Fatalf("Parse bad envelope: %v", err)
	}
	if err := ValidateCannedEnvelope(env, payload); err == nil {
		t.Fatalf("expected validation error for unknown evidence ref")
	}
}

func TestBuildCannedPromptIncludesBoundaries(t *testing.T) {
	payload := CannedAggregatePayload{
		Kind:     CannedToolReliabilityReview,
		DateFrom: "2025-01-15",
		DateTo:   "2025-01-16",
		Focus:    "Emphasize repeated shell failures.",
		EvidenceRefs: []CannedEvidenceRef{
			{ID: "signals:tool_health", Description: "tool health"},
		},
	}
	hash, err := CannedAggregateHash(payload)
	if err != nil {
		t.Fatalf("CannedAggregateHash: %v", err)
	}
	prompt, err := BuildCannedPrompt(payload, hash)
	if err != nil {
		t.Fatalf("BuildCannedPrompt: %v", err)
	}
	for _, want := range []string{
		"Output JSON only",
		"Do not recalculate, override",
		"tool_reliability_review",
		"User focus",
		"Emphasize repeated shell failures.",
		hash,
		"Aggregate payload JSON",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestBuildCannedCoachSummaryUsesCoachInsightFamilies(t *testing.T) {
	releasePrompt := "Generate release notes for the current build and verify changelog links"
	sessions := []db.Session{
		{
			ID:               "s1",
			Project:          "app",
			FirstMessage:     &releasePrompt,
			UserMessageCount: 3,
			HasToolCalls:     true,
		},
		{
			ID:               "s2",
			Project:          "app",
			FirstMessage:     &releasePrompt,
			UserMessageCount: 3,
			HasToolCalls:     true,
		},
		{
			ID:               "s3",
			Project:          "app",
			FirstMessage:     &releasePrompt,
			UserMessageCount: 3,
			HasToolCalls:     true,
		},
		{
			ID:               "s4",
			Project:          "api",
			FirstMessage:     new("Plan the auth migration with acceptance criteria and tests"),
			UserMessageCount: 4,
			HasContextData:   true,
		},
	}

	summary := BuildCannedCoachSummary(sessions)

	if summary.Source == "" || summary.SessionCount != len(sessions) {
		t.Fatalf("unexpected summary header: %+v", summary)
	}
	if summary.IntentDistribution["Planning"] == 0 ||
		summary.IntentDistribution["Implementation"] == 0 {
		t.Fatalf("missing Coach intent buckets: %+v", summary.IntentDistribution)
	}
	if summary.SpecDriven.Count == 0 || summary.SpecDriven.Rate <= 0 {
		t.Fatalf("missing spec-driven summary: %+v", summary.SpecDriven)
	}
	if summary.PromptMaturity.Score == 0 ||
		summary.PromptMaturity.Dimensions["verification_steps"] == 0 {
		t.Fatalf("missing prompt maturity summary: %+v", summary.PromptMaturity)
	}
	if len(summary.WorkflowClusters) != 1 {
		t.Fatalf("WorkflowClusters len = %d, want 1: %+v",
			len(summary.WorkflowClusters), summary.WorkflowClusters)
	}
	cluster := summary.WorkflowClusters[0]
	if cluster.Occurrences != 3 || cluster.Sessions != 3 ||
		!strings.Contains(cluster.Label, "Generate release notes") {
		t.Fatalf("unexpected workflow cluster: %+v", cluster)
	}
}

func TestCannedEvidenceRefsIncludesCoachSummary(t *testing.T) {
	coach := &CannedCoachSummary{
		SessionCount: 3,
		WorkflowClusters: []CannedCoachWorkflowCluster{
			{ID: "coach-workflow-0", Label: "Generate release notes"},
		},
	}

	refs := CannedEvidenceRefs(db.SignalsAnalyticsResponse{}, nil, coach)
	var ids []string
	for _, ref := range refs {
		ids = append(ids, ref.ID)
	}
	joined := strings.Join(ids, ",")
	for _, want := range []string{
		"coach:intent_distribution",
		"coach:prompt_maturity",
		"coach:spec_driven",
		"coach:workflow_clusters",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("refs missing %s: %v", want, ids)
		}
	}
}
