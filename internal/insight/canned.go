package insight

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/wesm/agentsview/internal/db"
)

const (
	CannedType          = "llm_canned"
	CannedSchemaVersion = "llm_insight.v1"
)

const maxCannedFieldRunes = 1200

// CannedKind is a fixed recommendation template identifier.
type CannedKind string

const (
	CannedPromptMaturityReview         CannedKind = "prompt_maturity_review"
	CannedContextSetupReview           CannedKind = "context_setup_review"
	CannedWorkflowHygieneReview        CannedKind = "workflow_hygiene_review"
	CannedToolReliabilityReview        CannedKind = "tool_reliability_review"
	CannedModelCostReview              CannedKind = "model_cost_review"
	CannedInstructionOpportunityReview CannedKind = "instruction_opportunity_review"
)

// ValidCannedKinds is the fixed template set for generated recommendations.
var ValidCannedKinds = map[CannedKind]bool{
	CannedPromptMaturityReview:         true,
	CannedContextSetupReview:           true,
	CannedWorkflowHygieneReview:        true,
	CannedToolReliabilityReview:        true,
	CannedModelCostReview:              true,
	CannedInstructionOpportunityReview: true,
}

type cannedTemplate struct {
	ID      string
	Version string
	Title   string
	Focus   string
}

var cannedTemplates = map[CannedKind]cannedTemplate{
	CannedPromptMaturityReview: {
		ID:      "prompt_maturity_review",
		Version: "2026-05-26",
		Title:   "Prompt Maturity Review",
		Focus: "Review prompt maturity using only the supplied deterministic aggregates. " +
			"Focus on missing constraints, unclear starts, repeated asks, and verification gaps.",
	},
	CannedContextSetupReview: {
		ID:      "context_setup_review",
		Version: "2026-05-26",
		Title:   "Context Setup Review",
		Focus: "Review context setup using only the supplied deterministic aggregates. " +
			"Focus on context pressure, compactions, and whether sessions have usable context evidence.",
	},
	CannedWorkflowHygieneReview: {
		ID:      "workflow_hygiene_review",
		Version: "2026-05-26",
		Title:   "Workflow Hygiene Review",
		Focus: "Review workflow hygiene using only the supplied deterministic aggregates. " +
			"Focus on outcomes, abandonment, retries, compactions, and sessions likely needing tighter loops.",
	},
	CannedToolReliabilityReview: {
		ID:      "tool_reliability_review",
		Version: "2026-05-26",
		Title:   "Tool Reliability Review",
		Focus: "Diagnose tool reliability using only the supplied deterministic aggregates. " +
			"Focus on failure signals, retries, edit churn, and likely process or environment causes.",
	},
	CannedModelCostReview: {
		ID:      "model_cost_review",
		Version: "2026-05-26",
		Title:   "Model and Cost Hygiene Review",
		Focus: "Review model and cost hygiene using only the supplied deterministic aggregates. " +
			"Focus on token mix, cache behavior, expensive sessions, and cost controls.",
	},
	CannedInstructionOpportunityReview: {
		ID:      "instruction_opportunity_review",
		Version: "2026-05-26",
		Title:   "Instruction Opportunity Review",
		Focus: "Suggest instruction, skill, or process improvements using only the supplied deterministic aggregates. " +
			"Label every suggestion as a draft opportunity, not a confirmed policy.",
	},
}

// CannedAggregatePayload is the deterministic input sent to the LLM.
type CannedAggregatePayload struct {
	Kind         CannedKind                  `json:"kind"`
	DateFrom     string                      `json:"date_from"`
	DateTo       string                      `json:"date_to"`
	Project      string                      `json:"project,omitempty"`
	Focus        string                      `json:"focus,omitempty"`
	Signals      db.SignalsAnalyticsResponse `json:"signals"`
	Usage        *CannedUsageSummary         `json:"usage,omitempty"`
	EvidenceRefs []CannedEvidenceRef         `json:"evidence_refs"`
}

type CannedUsageSummary struct {
	InputTokens         int                  `json:"input_tokens"`
	OutputTokens        int                  `json:"output_tokens"`
	CacheCreationTokens int                  `json:"cache_creation_tokens"`
	CacheReadTokens     int                  `json:"cache_read_tokens"`
	TotalCost           float64              `json:"total_cost"`
	CacheSavings        float64              `json:"cache_savings"`
	TopSessionsByCost   []db.TopSessionEntry `json:"top_sessions_by_cost,omitempty"`
}

type CannedEvidenceRef struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// CannedRecommendationEnvelope is the strict model-output schema.
type CannedRecommendationEnvelope struct {
	SchemaVersion   string                 `json:"schema_version"`
	Kind            CannedKind             `json:"kind"`
	Summary         string                 `json:"summary"`
	Confidence      string                 `json:"confidence"`
	Recommendations []CannedRecommendation `json:"recommendations"`
	Risks           []CannedRisk           `json:"risks"`
	EvidenceRefs    []string               `json:"evidence_refs"`
}

type CannedRecommendation struct {
	Title        string   `json:"title"`
	Rationale    string   `json:"rationale"`
	Actions      []string `json:"actions"`
	EvidenceRefs []string `json:"evidence_refs"`
	Impact       string   `json:"impact"`
	Effort       string   `json:"effort"`
}

type CannedRisk struct {
	Title        string   `json:"title"`
	Explanation  string   `json:"explanation"`
	EvidenceRefs []string `json:"evidence_refs"`
}

// CannedProvenance is server-owned metadata, not model output.
type CannedProvenance struct {
	TemplateID      string            `json:"template_id"`
	TemplateVersion string            `json:"template_version"`
	SchemaVersion   string            `json:"schema_version"`
	AggregateHash   string            `json:"aggregate_hash"`
	CacheKey        string            `json:"cache_key"`
	CacheStatus     string            `json:"cache_status"`
	GeneratedAt     string            `json:"generated_at"`
	DateFrom        string            `json:"date_from"`
	DateTo          string            `json:"date_to"`
	Project         string            `json:"project,omitempty"`
	Filters         map[string]string `json:"filters"`
	Agent           string            `json:"agent"`
	Model           string            `json:"model,omitempty"`
	SourceVersions  map[string]string `json:"source_versions"`
}

func CannedTemplate(kind CannedKind) (cannedTemplate, bool) {
	t, ok := cannedTemplates[kind]
	return t, ok
}

func CannedAggregateHash(payload CannedAggregatePayload) (string, error) {
	data, err := canonicalJSON(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func CannedCacheKey(
	kind CannedKind,
	dateFrom, dateTo, project, requestedAgent, focus, aggregateHash string,
) (string, error) {
	t, ok := CannedTemplate(kind)
	if !ok {
		return "", fmt.Errorf("unknown canned insight kind: %s", kind)
	}
	focusSum := sha256.Sum256([]byte(strings.TrimSpace(focus)))
	input := map[string]string{
		"kind":             string(kind),
		"date_from":        dateFrom,
		"date_to":          dateTo,
		"project":          project,
		"requested_agent":  requestedAgent,
		"template_version": t.Version,
		"schema_version":   CannedSchemaVersion,
		"aggregate_hash":   aggregateHash,
		"focus_hash":       hex.EncodeToString(focusSum[:]),
	}
	data, err := canonicalJSON(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return string(kind) + ":" + hex.EncodeToString(sum[:]), nil
}

func BuildCannedPrompt(
	payload CannedAggregatePayload,
	aggregateHash string,
) (string, error) {
	t, ok := CannedTemplate(payload.Kind)
	if !ok {
		return "", fmt.Errorf("unknown canned insight kind: %s", payload.Kind)
	}
	data, err := canonicalJSON(payload)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("You are generating an opt-in AI recommendation for agentsview.\n")
	b.WriteString("Use only the deterministic aggregate JSON supplied below. ")
	b.WriteString("Do not inspect or infer from raw transcripts. ")
	b.WriteString("Do not recalculate, override, or propose changes to canonical health scores or signal rows. ")
	b.WriteString("Output JSON only, with no markdown fences or prose outside JSON.\n\n")
	b.WriteString("Template: ")
	b.WriteString(t.Title)
	b.WriteString("\nTemplate ID: ")
	b.WriteString(t.ID)
	b.WriteString("\nTemplate Version: ")
	b.WriteString(t.Version)
	b.WriteString("\nSchema Version: ")
	b.WriteString(CannedSchemaVersion)
	b.WriteString("\nAggregate Hash: ")
	b.WriteString(aggregateHash)
	b.WriteString("\n\nFocus:\n")
	b.WriteString(t.Focus)
	b.WriteString("\n\nRequired JSON shape:\n")
	b.WriteString(`{"schema_version":"llm_insight.v1","kind":"`)
	b.WriteString(string(payload.Kind))
	b.WriteString(`","summary":"...","confidence":"low|medium|high","recommendations":[{"title":"...","rationale":"...","actions":["..."],"evidence_refs":["..."],"impact":"low|medium|high","effort":"low|medium|high"}],"risks":[{"title":"...","explanation":"...","evidence_refs":["..."]}],"evidence_refs":["..."]}`)
	b.WriteString("\n\nRules:\n")
	b.WriteString("- Include 1 to 5 recommendations.\n")
	b.WriteString("- Include at most 4 risks.\n")
	b.WriteString("- Every recommendation and risk must cite evidence_refs from the aggregate payload.\n")
	b.WriteString("- Do not emit evidence refs that are absent from the payload.\n")
	b.WriteString("- Keep fields concise and action-oriented.\n")
	b.WriteString("- If evidence is weak or empty, lower confidence and say what deterministic data is missing.\n\n")
	b.WriteString("Aggregate payload JSON:\n")
	b.Write(data)
	return b.String(), nil
}

func ParseCannedEnvelope(raw string) (CannedRecommendationEnvelope, error) {
	clean := strings.TrimSpace(raw)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")
	clean = strings.TrimSpace(clean)

	var out CannedRecommendationEnvelope
	dec := json.NewDecoder(strings.NewReader(clean))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("parsing canned insight JSON: %w", err)
	}
	var extra struct{}
	if err := dec.Decode(&extra); err == nil {
		return out, errors.New("canned insight JSON contains trailing values")
	} else if !errors.Is(err, io.EOF) {
		return out, fmt.Errorf("canned insight JSON has trailing data: %w", err)
	}
	return out, nil
}

func ValidateCannedEnvelope(
	out CannedRecommendationEnvelope,
	payload CannedAggregatePayload,
) error {
	if out.SchemaVersion != CannedSchemaVersion {
		return fmt.Errorf("schema_version = %q, want %q", out.SchemaVersion, CannedSchemaVersion)
	}
	if out.Kind != payload.Kind {
		return fmt.Errorf("kind = %q, want %q", out.Kind, payload.Kind)
	}
	if !validConfidence(out.Confidence) {
		return fmt.Errorf("invalid confidence: %q", out.Confidence)
	}
	if strings.TrimSpace(out.Summary) == "" {
		return errors.New("summary is required")
	}
	if len(out.Recommendations) == 0 || len(out.Recommendations) > 5 {
		return fmt.Errorf("recommendations count = %d, want 1..5", len(out.Recommendations))
	}
	if len(out.Risks) > 4 {
		return fmt.Errorf("risks count = %d, want <= 4", len(out.Risks))
	}

	allowed := make(map[string]bool, len(payload.EvidenceRefs))
	for _, ref := range payload.EvidenceRefs {
		allowed[ref.ID] = true
	}
	if len(allowed) == 0 {
		allowed["aggregate:empty"] = true
	}

	for _, ref := range out.EvidenceRefs {
		if !allowed[ref] {
			return fmt.Errorf("unknown envelope evidence_ref: %s", ref)
		}
	}
	for i, rec := range out.Recommendations {
		if err := validateCannedRecommendation(rec, allowed); err != nil {
			return fmt.Errorf("recommendation %d: %w", i, err)
		}
	}
	for i, risk := range out.Risks {
		if err := validateCannedRisk(risk, allowed); err != nil {
			return fmt.Errorf("risk %d: %w", i, err)
		}
	}
	return validateCannedFieldSizes(out)
}

func RenderCannedMarkdown(
	out CannedRecommendationEnvelope,
	prov CannedProvenance,
) string {
	var b strings.Builder
	b.WriteString("# AI-generated recommendation\n\n")
	b.WriteString("> Generated recommendation text. Deterministic health scores and signal rows were not modified.\n\n")
	b.WriteString("## Summary\n\n")
	b.WriteString(out.Summary)
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf(
		"_Kind: %s. Confidence: %s. Template: %s@%s. Aggregate: `%s`._\n\n",
		out.Kind, out.Confidence, prov.TemplateID, prov.TemplateVersion,
		shortHash(prov.AggregateHash),
	))
	b.WriteString("## Recommendations\n\n")
	for i, rec := range out.Recommendations {
		fmt.Fprintf(&b, "%d. **%s**\n", i+1, rec.Title)
		fmt.Fprintf(&b, "   - Rationale: %s\n", rec.Rationale)
		fmt.Fprintf(&b, "   - Impact: %s. Effort: %s.\n", rec.Impact, rec.Effort)
		if len(rec.Actions) > 0 {
			b.WriteString("   - Actions:\n")
			for _, action := range rec.Actions {
				fmt.Fprintf(&b, "     - %s\n", action)
			}
		}
		if len(rec.EvidenceRefs) > 0 {
			fmt.Fprintf(&b, "   - Evidence: `%s`\n", strings.Join(rec.EvidenceRefs, "`, `"))
		}
	}
	if len(out.Risks) > 0 {
		b.WriteString("\n## Risks\n\n")
		for _, risk := range out.Risks {
			fmt.Fprintf(&b, "- **%s**: %s", risk.Title, risk.Explanation)
			if len(risk.EvidenceRefs) > 0 {
				fmt.Fprintf(&b, " (`%s`)", strings.Join(risk.EvidenceRefs, "`, `"))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func NewCannedProvenance(
	payload CannedAggregatePayload,
	aggregateHash, cacheKey, cacheStatus, agent, model string,
	generatedAt time.Time,
) (CannedProvenance, error) {
	t, ok := CannedTemplate(payload.Kind)
	if !ok {
		return CannedProvenance{}, fmt.Errorf("unknown canned insight kind: %s", payload.Kind)
	}
	return CannedProvenance{
		TemplateID:      t.ID,
		TemplateVersion: t.Version,
		SchemaVersion:   CannedSchemaVersion,
		AggregateHash:   aggregateHash,
		CacheKey:        cacheKey,
		CacheStatus:     cacheStatus,
		GeneratedAt:     generatedAt.UTC().Format(time.RFC3339Nano),
		DateFrom:        payload.DateFrom,
		DateTo:          payload.DateTo,
		Project:         payload.Project,
		Filters: map[string]string{
			"project": payload.Project,
		},
		Agent: agent,
		Model: model,
		SourceVersions: map[string]string{
			"signals_analytics": "v1",
			"usage_analytics":   "v1",
		},
	}, nil
}

func canonicalJSON(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func validConfidence(v string) bool {
	switch v {
	case "low", "medium", "high":
		return true
	default:
		return false
	}
}

func validateCannedRecommendation(
	rec CannedRecommendation,
	allowed map[string]bool,
) error {
	if strings.TrimSpace(rec.Title) == "" {
		return errors.New("title is required")
	}
	if strings.TrimSpace(rec.Rationale) == "" {
		return errors.New("rationale is required")
	}
	if len(rec.Actions) == 0 || len(rec.Actions) > 5 {
		return fmt.Errorf("actions count = %d, want 1..5", len(rec.Actions))
	}
	if !validConfidence(rec.Impact) {
		return fmt.Errorf("invalid impact: %q", rec.Impact)
	}
	if !validConfidence(rec.Effort) {
		return fmt.Errorf("invalid effort: %q", rec.Effort)
	}
	if len(rec.EvidenceRefs) == 0 {
		return errors.New("evidence_refs is required")
	}
	return validateEvidenceRefs(rec.EvidenceRefs, allowed)
}

func validateCannedRisk(risk CannedRisk, allowed map[string]bool) error {
	if strings.TrimSpace(risk.Title) == "" {
		return errors.New("title is required")
	}
	if strings.TrimSpace(risk.Explanation) == "" {
		return errors.New("explanation is required")
	}
	if len(risk.EvidenceRefs) == 0 {
		return errors.New("evidence_refs is required")
	}
	return validateEvidenceRefs(risk.EvidenceRefs, allowed)
}

func validateEvidenceRefs(refs []string, allowed map[string]bool) error {
	for _, ref := range refs {
		if !allowed[ref] {
			return fmt.Errorf("unknown evidence_ref: %s", ref)
		}
	}
	return nil
}

func validateCannedFieldSizes(out CannedRecommendationEnvelope) error {
	var fields []string
	fields = append(fields, out.Summary)
	for _, rec := range out.Recommendations {
		fields = append(fields, rec.Title, rec.Rationale, rec.Impact, rec.Effort)
		fields = append(fields, rec.Actions...)
	}
	for _, risk := range out.Risks {
		fields = append(fields, risk.Title, risk.Explanation)
	}
	for _, f := range fields {
		if len([]rune(f)) > maxCannedFieldRunes {
			return errors.New("canned insight field exceeds maximum length")
		}
	}
	return nil
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func CannedEvidenceRefs(
	signals db.SignalsAnalyticsResponse,
	usage *CannedUsageSummary,
) []CannedEvidenceRef {
	refs := []CannedEvidenceRef{
		{ID: "signals:score_distribution", Description: "Scored, unscored, average health score, and grade distribution."},
		{ID: "signals:outcomes", Description: "Outcome and outcome confidence distribution."},
		{ID: "signals:tool_health", Description: "Tool failure, retry, edit churn, and failure-rate aggregates."},
		{ID: "signals:context_health", Description: "Compaction, context pressure, and context-data aggregates."},
		{ID: "signals:trend", Description: "Daily signal trend buckets."},
	}
	if len(signals.ByAgent) > 0 {
		refs = append(refs, CannedEvidenceRef{ID: "signals:by_agent", Description: "Signal aggregates grouped by agent."})
	}
	if len(signals.ByProject) > 0 {
		refs = append(refs, CannedEvidenceRef{ID: "signals:by_project", Description: "Signal aggregates grouped by project."})
	}
	if usage != nil {
		refs = append(refs, CannedEvidenceRef{ID: "usage:totals", Description: "Deterministic token, cache, and cost totals."})
		if len(usage.TopSessionsByCost) > 0 {
			refs = append(refs, CannedEvidenceRef{ID: "usage:top_sessions_by_cost", Description: "Deterministic top sessions by cost."})
		}
	}
	if signals.ScoredSessions == 0 && signals.UnscoredSessions == 0 {
		return []CannedEvidenceRef{{ID: "aggregate:empty", Description: "No deterministic aggregate rows matched the selected filters."}}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return refs
}
