import { describe, expect, it } from "vitest";
import type { SignalsAnalyticsResponse } from "../../api/types.js";
import {
  buildQualityPatterns,
  buildQualitySummary,
  buildRuleBasedRecommendations,
} from "./qualityPatterns.js";

function makeSignals(
  overrides: Partial<SignalsAnalyticsResponse> = {},
): SignalsAnalyticsResponse {
  return {
    scored_sessions: 5,
    unscored_sessions: 1,
    grade_distribution: { A: 2, B: 1, D: 1, F: 1 },
    avg_health_score: 72.4,
    outcome_distribution: {
      completed: 3,
      errored: 1,
      abandoned: 1,
    },
    outcome_confidence_distribution: { high: 4, medium: 1 },
    tool_health: {
      total_failure_signals: 4,
      total_retries: 2,
      total_edit_churn: 1,
      sessions_with_failures: 2,
      failure_rate: 33.3,
    },
    context_health: {
      avg_compaction_count: 0.8,
      sessions_with_compaction: 2,
      mid_task_compaction_count: 1,
      sessions_with_mid_task_compaction: 1,
      sessions_with_context_data: 4,
      avg_context_pressure: 0.55,
      high_pressure_sessions: 1,
    },
    quality_health: {
      computed_sessions: 4,
      totals: {
        short_prompt_count: 3,
        unstructured_start: 1,
        missing_success_criteria_count: 2,
        missing_verification_count: 1,
        duplicate_prompt_count: 1,
        no_code_context_count: 1,
        runaway_tool_loop_count: 0,
      },
      sessions_with_signal: {
        short_prompt_count: 2,
        unstructured_start: 1,
        missing_success_criteria_count: 2,
        missing_verification_count: 1,
        duplicate_prompt_count: 1,
        no_code_context_count: 1,
        runaway_tool_loop_count: 0,
      },
    },
    trend: [
      {
        date: "2026-05-20",
        session_count: 2,
        avg_health_score: 80,
        completed: 2,
        errored: 0,
        abandoned: 0,
        avg_failure_signals: 0.5,
      },
      {
        date: "2026-05-21",
        session_count: 3,
        avg_health_score: 62,
        completed: 1,
        errored: 1,
        abandoned: 1,
        avg_failure_signals: 1.5,
      },
    ],
    by_agent: [
      {
        agent: "codex",
        session_count: 3,
        avg_health_score: 68,
        completed_rate: 66.7,
        avg_failure_signals: 1.2,
      },
    ],
    by_project: [
      {
        project: "agentsview",
        session_count: 4,
        avg_health_score: 70,
        completed_rate: 75,
        avg_failure_signals: 1.4,
      },
    ],
    ...overrides,
  };
}

describe("quality pattern transforms", () => {
  it("summarizes score distribution and low-quality sessions", () => {
    const summary = buildQualitySummary(makeSignals());

    expect(summary.totalSessions).toBe(6);
    expect(summary.computedQualitySessions).toBe(4);
    expect(summary.lowQualitySessions).toBe(2);
    expect(summary.scoreDistribution.map((b) => b.grade)).toEqual([
      "A",
      "B",
      "D",
      "F",
    ]);
  });

  it("builds prompt maturity from real Phase 3 quality health fields", () => {
    const prompt = buildQualityPatterns(makeSignals())[0];

    expect(prompt).toBeDefined();
    if (!prompt) return;
    expect(prompt.title).toBe("Prompt maturity");
    expect(prompt.totalSessions).toBe(4);
    expect(prompt.affectedSessions).toBe(2);
    expect(prompt.drivers).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          id: "missing_success_criteria_count",
          total: 2,
          sessions: 2,
        }),
      ]),
    );
  });

  it("does not recommend unavailable or clear patterns", () => {
    const signals = makeSignals({
      outcome_distribution: { completed: 6 },
      tool_health: {
        total_failure_signals: 0,
        total_retries: 0,
        total_edit_churn: 0,
        sessions_with_failures: 0,
        failure_rate: 0,
      },
      context_health: {
        avg_compaction_count: 0,
        sessions_with_compaction: 0,
        mid_task_compaction_count: 0,
        sessions_with_mid_task_compaction: 0,
        sessions_with_context_data: 0,
        avg_context_pressure: null,
        high_pressure_sessions: 0,
      },
      quality_health: {
        computed_sessions: 0,
        totals: {
          short_prompt_count: 0,
          unstructured_start: 0,
          missing_success_criteria_count: 0,
          missing_verification_count: 0,
          duplicate_prompt_count: 0,
          no_code_context_count: 0,
          runaway_tool_loop_count: 0,
        },
        sessions_with_signal: {
          short_prompt_count: 0,
          unstructured_start: 0,
          missing_success_criteria_count: 0,
          missing_verification_count: 0,
          duplicate_prompt_count: 0,
          no_code_context_count: 0,
          runaway_tool_loop_count: 0,
        },
      },
    });

    expect(
      buildRuleBasedRecommendations(buildQualityPatterns(signals)),
    ).toEqual([]);
  });
});
