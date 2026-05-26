package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"go.kenn.io/agentsview/internal/db"
	"go.kenn.io/agentsview/internal/insight"
	"go.kenn.io/agentsview/internal/timeutil"
)

var validInsightTypes = map[string]bool{
	"daily_activity":   true,
	"agent_analysis":   true,
	insight.CannedType: true,
}

type generateInsightRequest struct {
	Type         string `json:"type"`
	DateFrom     string `json:"date_from"`
	DateTo       string `json:"date_to"`
	Project      string `json:"project,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	Agent        string `json:"agent,omitempty"`
	Kind         string `json:"kind,omitempty"`
	LLMOptIn     bool   `json:"llm_opt_in,omitempty"`
	ForceRefresh bool   `json:"force_refresh,omitempty"`
}

func insightGenerateClientMessage(
	agent string, err error,
) string {
	if err == nil {
		return fmt.Sprintf("%s generation failed", agent)
	}
	msg := err.Error()
	// Strip stderr dump after newline for the short client message; full details
	// are in the log stream.
	if idx := strings.Index(msg, "\nstderr:"); idx > 0 {
		msg = msg[:idx]
	}
	if idx := strings.Index(msg, "\nraw:"); idx > 0 {
		msg = msg[:idx]
	}
	return msg
}

func (s *Server) humaGenerateCannedInsight(
	req generateInsightRequest,
) (*huma.StreamResponse, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	kind := insight.CannedKind(req.Kind)
	if !insight.ValidCannedKinds[kind] {
		return nil, apiError(http.StatusBadRequest,
			"invalid kind: unsupported canned insight")
	}
	if req.Type != "" && req.Type != insight.CannedType {
		return nil, apiError(http.StatusBadRequest,
			"type must be llm_canned for canned insights")
	}
	if !req.LLMOptIn {
		return nil, apiError(http.StatusBadRequest,
			"llm_opt_in must be true for canned insights")
	}
	if len([]rune(req.Prompt)) > insight.MaxCannedFocusRunes {
		return nil, apiError(http.StatusBadRequest,
			"prompt is too long for canned insight focus")
	}
	if !timeutil.IsValidDate(req.DateFrom) {
		return nil, apiError(http.StatusBadRequest,
			"invalid date_from: use YYYY-MM-DD")
	}
	if !timeutil.IsValidDate(req.DateTo) {
		return nil, apiError(http.StatusBadRequest,
			"invalid date_to: use YYYY-MM-DD")
	}
	if req.DateTo < req.DateFrom {
		return nil, apiError(http.StatusBadRequest,
			"date_to must be >= date_from")
	}
	if req.Agent == "" {
		req.Agent = "claude"
	}
	if !insight.ValidAgents[req.Agent] {
		return nil, apiError(http.StatusBadRequest,
			"invalid agent: must be one of "+
				strings.Join(insight.ValidAgentNames, ", "))
	}

	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		stream, ok := newHumaSSEStream(hctx)
		if !ok {
			writeHumaJSON(hctx, http.StatusInternalServerError,
				apiErrorResponse{Message: "streaming not supported"})
			return
		}
		s.generateCannedInsight(hctx.Context(), stream, kind, req)
	}}, nil
}

func (s *Server) generateCannedInsight(
	ctx context.Context,
	stream *SSEStream,
	kind insight.CannedKind,
	req generateInsightRequest,
) {
	sendJSON := func(event string, v any) bool {
		return stream.SendJSON(event, v)
	}
	status := func(phase string) bool {
		return sendJSON("status", map[string]string{
			"phase": phase,
		})
	}

	if !status("building_payload") {
		return
	}
	payload, aggregateHash, cacheKey, err := s.buildCannedPayload(
		ctx, kind, req,
	)
	if err != nil {
		log.Printf("canned insight payload error: %v", err)
		sendJSON("error", map[string]string{
			"message": "failed to build canned insight payload",
		})
		return
	}

	if !req.ForceRefresh {
		cached, err := s.db.GetCachedInsight(ctx, cacheKey)
		if err != nil {
			log.Printf("canned insight cache lookup error: %v", err)
			sendJSON("error", map[string]string{
				"message": "failed to check insight cache",
			})
			return
		}
		if cached != nil {
			markInsightCacheHit(cached)
			if !status("cache_hit") {
				return
			}
			sendJSON("done", cached)
			return
		}
	}

	prompt, err := insight.BuildCannedPrompt(payload, aggregateHash)
	if err != nil {
		log.Printf("canned insight prompt error: %v", err)
		sendJSON("error", map[string]string{
			"message": "failed to build canned prompt",
		})
		return
	}

	if !status("generating") {
		return
	}
	genCtx, cancel := context.WithTimeout(
		ctx, 3*time.Minute,
	)
	defer cancel()
	result, err := s.generateStreamFunc(
		genCtx, req.Agent, prompt, nil,
	)
	if err != nil {
		log.Printf("canned insight generate error: %v", err)
		sendJSON("error", map[string]string{
			"message": insightGenerateClientMessage(
				req.Agent, err,
			),
		})
		return
	}

	if !status("validating") {
		return
	}
	envelope, err := insight.ParseCannedEnvelope(result.Content)
	if err != nil {
		log.Printf("canned insight parse error: %v", err)
		sendJSON("error", map[string]string{
			"message": "generated insight was not valid JSON",
		})
		return
	}
	if err := insight.ValidateCannedEnvelope(envelope, payload); err != nil {
		log.Printf("canned insight validation error: %v", err)
		sendJSON("error", map[string]string{
			"message": "generated insight failed validation",
		})
		return
	}

	if !status("saving") {
		return
	}
	model := result.Model
	prov, err := insight.NewCannedProvenance(
		payload, aggregateHash, cacheKey, "fresh",
		result.Agent, model, time.Now(),
	)
	if err != nil {
		log.Printf("canned insight provenance error: %v", err)
		sendJSON("error", map[string]string{
			"message": "failed to build insight provenance",
		})
		return
	}
	provJSON, err := json.Marshal(prov)
	if err != nil {
		log.Printf("canned insight provenance JSON error: %v", err)
		sendJSON("error", map[string]string{
			"message": "failed to encode insight provenance",
		})
		return
	}
	structuredJSON, err := json.Marshal(envelope)
	if err != nil {
		log.Printf("canned insight structured JSON error: %v", err)
		sendJSON("error", map[string]string{
			"message": "failed to encode generated insight",
		})
		return
	}

	var project *string
	if req.Project != "" {
		project = &req.Project
	}
	var modelPtr *string
	if model != "" {
		modelPtr = &model
	}
	var promptPtr *string
	if req.Prompt != "" {
		promptPtr = &req.Prompt
	}

	id, err := s.db.InsertInsight(db.Insight{
		Type:            insight.CannedType,
		DateFrom:        req.DateFrom,
		DateTo:          req.DateTo,
		Project:         project,
		Agent:           result.Agent,
		Model:           modelPtr,
		Prompt:          promptPtr,
		Content:         insight.RenderCannedMarkdown(envelope, prov),
		Kind:            string(kind),
		SchemaVersion:   insight.CannedSchemaVersion,
		TemplateID:      prov.TemplateID,
		TemplateVersion: prov.TemplateVersion,
		AggregateHash:   aggregateHash,
		CacheKey:        cacheKey,
		CacheStatus:     "fresh",
		ProvenanceJSON:  string(provJSON),
		StructuredJSON:  string(structuredJSON),
	})
	if err != nil {
		log.Printf("canned insight insert error: %v", err)
		sendJSON("error", map[string]string{
			"message": "failed to save insight",
		})
		return
	}

	saved, err := s.db.GetInsight(ctx, id)
	if err != nil || saved == nil {
		log.Printf("canned insight get error: id=%d err=%v",
			id, err)
		sendJSON("error", map[string]string{
			"message": "failed to retrieve saved insight",
		})
		return
	}
	sendJSON("done", saved)
}

func markInsightCacheHit(s *db.Insight) {
	if s == nil {
		return
	}
	s.CacheStatus = "hit"
	if strings.TrimSpace(s.ProvenanceJSON) == "" {
		return
	}
	var prov map[string]any
	if err := json.Unmarshal([]byte(s.ProvenanceJSON), &prov); err != nil {
		return
	}
	prov["cache_status"] = "hit"
	data, err := json.Marshal(prov)
	if err != nil {
		return
	}
	s.ProvenanceJSON = string(data)
}

func (s *Server) buildCannedPayload(
	ctx context.Context,
	kind insight.CannedKind,
	req generateInsightRequest,
) (insight.CannedAggregatePayload, string, string, error) {
	analyticsFilter := db.AnalyticsFilter{
		From:             req.DateFrom,
		To:               req.DateTo,
		Project:          req.Project,
		Timezone:         "UTC",
		ExcludeOneShot:   true,
		ExcludeAutomated: true,
	}
	signals, err := s.db.GetAnalyticsSignals(ctx, analyticsFilter)
	if err != nil {
		return insight.CannedAggregatePayload{}, "", "", err
	}

	usageFilter := db.UsageFilter{
		From:             req.DateFrom,
		To:               req.DateTo,
		Project:          req.Project,
		Timezone:         "UTC",
		ExcludeOneShot:   true,
		ExcludeAutomated: true,
		Breakdowns:       false,
	}
	usageResult, err := s.db.GetDailyUsage(ctx, usageFilter)
	if err != nil {
		return insight.CannedAggregatePayload{}, "", "", err
	}
	topSessions, err := s.db.GetTopSessionsByCost(ctx, usageFilter, 5)
	if err != nil {
		return insight.CannedAggregatePayload{}, "", "", err
	}
	usageSummary := &insight.CannedUsageSummary{
		InputTokens:         usageResult.Totals.InputTokens,
		OutputTokens:        usageResult.Totals.OutputTokens,
		CacheCreationTokens: usageResult.Totals.CacheCreationTokens,
		CacheReadTokens:     usageResult.Totals.CacheReadTokens,
		TotalCost:           usageResult.Totals.TotalCost,
		CacheSavings:        usageResult.Totals.CacheSavings,
		TopSessionsByCost:   topSessions,
	}

	payload := insight.CannedAggregatePayload{
		Kind:     kind,
		DateFrom: req.DateFrom,
		DateTo:   req.DateTo,
		Project:  req.Project,
		Focus:    req.Prompt,
		Signals:  signals,
		Usage:    usageSummary,
	}
	payload.EvidenceRefs = insight.CannedEvidenceRefs(
		signals, usageSummary,
	)

	aggregateHash, err := insight.CannedAggregateHash(payload)
	if err != nil {
		return insight.CannedAggregatePayload{}, "", "", err
	}
	cacheKey, err := insight.CannedCacheKey(
		kind, req.DateFrom, req.DateTo, req.Project,
		req.Agent, req.Prompt, aggregateHash,
	)
	if err != nil {
		return insight.CannedAggregatePayload{}, "", "", err
	}
	return payload, aggregateHash, cacheKey, nil
}
