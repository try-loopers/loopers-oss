package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/try-loopers/loopers-oss/internal/a2a"
	"github.com/try-loopers/loopers-oss/internal/blastradius"
	"github.com/try-loopers/loopers-oss/internal/budget"
	"github.com/try-loopers/loopers-oss/internal/event"
	"github.com/try-loopers/loopers-oss/internal/inspector"
	"github.com/try-loopers/loopers-oss/internal/keyring"
	"github.com/try-loopers/loopers-oss/internal/logging"
	"github.com/try-loopers/loopers-oss/internal/policy"
	"github.com/try-loopers/loopers-oss/internal/pricing"
	"github.com/try-loopers/loopers-oss/internal/provider"
	"github.com/try-loopers/loopers-oss/internal/proxy"
	"github.com/try-loopers/loopers-oss/internal/riskprofile"
	"github.com/try-loopers/loopers-oss/internal/session"
	"github.com/try-loopers/loopers-oss/pkg/api"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (s *Server) handleProxy(c *gin.Context, providerName string) {
	startTime := time.Now()

	// 1. Auth check
	var meta *keyring.KeyMetadata
	var keyHash string
	reqID := c.GetString("RequestID")

	if claims, exists := c.Get(JWTClaimsCtx); exists {
		meta = claims.(*keyring.KeyMetadata)
		if keyHashCtx, ok := c.Get("KeyHash"); ok {
			keyHash = keyHashCtx.(string)
		} else {
			keyHash = keyring.HashKey(meta.Name)
		}

		// DPoP Validation
		dpopHeader := c.GetHeader("DPoP")
		if dpopHeader != "" {
			requestURL := "https://" + c.Request.Host + c.Request.URL.Path
			if pub := viper.GetString("server.public_url"); pub != "" {
				requestURL = strings.TrimSuffix(pub, "/") + c.Request.URL.Path
			}
			_, err := keyring.ValidateDPoPAndReplay(c.Request.Context(), s.redis.GetUnderlyingClient(), dpopHeader, c.Request.Method, requestURL, meta.Jkt)
			if err != nil {
				if err.Error() == "DPoP token replay detected" {
					zspAuthFailuresTotal.WithLabelValues("dpop_replayed").Inc()
				} else {
					zspAuthFailuresTotal.WithLabelValues("dpop_invalid").Inc()
				}
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
				return
			}
		} else if meta.Jkt != "" {
			// Required DPoP but missing
			zspAuthFailuresTotal.WithLabelValues("dpop_missing").Inc()
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing DPoP header"})
			return
		}

	} else if keyHashCtx, exists := c.Get("KeyHash"); exists {
		keyHash = keyHashCtx.(string)
		var err error
		meta, err = keyring.GetKeyMetadata(c.Request.Context(), s.redis.GetUnderlyingClient(), keyHash)
		if err != nil {
			if err.Error() == "key does not exist" {
				requestsTotal.WithLabelValues(providerName, "unknown", "401").Inc()
				if s.alerter != nil {
					go s.alerter.TriggerAuthFail(detachedTraceContext(c.Request.Context()), reqID, "Key not registered")
				}
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: key not registered"})
			} else {
				logging.Logger.Error().Err(err).Msg("failed to get key metadata")
				requestsTotal.WithLabelValues(providerName, "unknown", "503").Inc()
				if s.alerter != nil {
					go s.alerter.TriggerFailClosed(detachedTraceContext(c.Request.Context()), reqID, "Failed to get key metadata")
				}
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "Service Unavailable: internal error"})
			}
			return
		}
	} else {
		requestsTotal.WithLabelValues(providerName, "unknown", "401").Inc()
		if s.alerter != nil {
			go s.alerter.TriggerAuthFail(detachedTraceContext(c.Request.Context()), reqID, "Missing Authorization header")
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing Authorization header"})
		return
	}
	if !strings.EqualFold(meta.Active, "true") {
		requestsTotal.WithLabelValues(providerName, "unknown", "401").Inc()
		if s.alerter != nil {
			go s.alerter.TriggerAuthFail(detachedTraceContext(c.Request.Context()), reqID, "Key revoked")
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: key has been revoked"})
		return
	}

	// Quarantine Check: enforce at auth layer before any policy evaluation
	quarantineKey := "loopers:quarantine:" + keyHash
	if exists, _ := s.redis.GetUnderlyingClient().Exists(c.Request.Context(), quarantineKey).Result(); exists > 0 {
		event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
			EventType: "quarantine_active",
			KeyHash:   keyHash,
			Provider:  providerName,
			Reason:    "quarantine_active",
			Detail:    "Agent is under active policy quarantine",
			RequestID: reqID,
		})
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "Agent is currently quarantined by security policy",
			"type":  "quarantine_active",
		})
		return
	}

	// Risk Profile Check: enforce behavioral thresholds before any policy evaluation
	var rp *riskprofile.AgentRiskProfile
	if s.riskProfileCfg.Enabled && s.redis != nil {
		loaded, rpErr := riskprofile.GetProfile(c.Request.Context(), s.redis.GetUnderlyingClient(), keyHash)
		if rpErr == nil && loaded != nil {
			rp = loaded
			if rp.RiskScore > s.riskProfileCfg.PermanentBlockThreshold {
				event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
					EventType: "agent_risk_blocked",
					KeyHash:   keyHash,
					Provider:  providerName,
					Reason:    "agent_risk_blocked",
					Detail:    fmt.Sprintf("Agent risk score %d exceeds permanent block threshold %d", rp.RiskScore, s.riskProfileCfg.PermanentBlockThreshold),
					RequestID: reqID,
				})
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "Agent permanently blocked due to high behavioral risk",
					"type":  "agent_risk_blocked",
				})
				return
			}
			if rp.RiskScore > s.riskProfileCfg.AutoQuarantineThreshold {
				dur := 1 * time.Hour
				_ = s.redis.GetUnderlyingClient().Set(c.Request.Context(), "loopers:quarantine:"+keyHash, "1", dur).Err()
				event.EmitQuarantineEvent(c.Request.Context(), event.QuarantineEvent{
					KeyHash:       keyHash,
					Reason:        fmt.Sprintf("Agent risk score %d exceeded auto-quarantine threshold %d", rp.RiskScore, s.riskProfileCfg.AutoQuarantineThreshold),
					QuarantineFor: "1h",
					RequestID:     reqID,
				})
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "Agent is currently quarantined by security policy",
					"type":  "quarantine_active",
				})
				return
			}
		}
	}

	if meta.Provider != providerName {
		requestsTotal.WithLabelValues(providerName, "unknown", "400").Inc()
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error": "key not authorized for this endpoint",
		})
		return
	}

	if allowedProviders := meta.ParseAllowedProviders(); len(allowedProviders) > 0 {
		if !allowedProviders[providerName] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "key not authorized for this provider",
				"type":  "provider_not_allowed",
			})
			return
		}
	}

	if s.otelEnabled {
		span := trace.SpanFromContext(c.Request.Context())
		span.SetAttributes(
			attribute.String("gen_ai.system", providerName),
			attribute.String("loopers.budget.key_hash", keyHash),
		)
		if meta.AgentName != "" {
			span.SetAttributes(attribute.String("loopers.agent.name", meta.AgentName))
		}
	}

	if s.rateLimiter != nil {
		allowed, remaining, rlErr := s.rateLimiter.Check(c.Request.Context(), keyHash)
		if rlErr != nil {
			logging.Logger.Error().Err(rlErr).Msg("rate_limiter_error")
			event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
				EventType: "rate_limit_degraded",
				KeyHash:   keyHash,
				Provider:  providerName,
				Reason:    "rate_limiter_error",
				Detail:    rlErr.Error(),
			})
			failClosed := true // Default to true per security strategy
			if viper.IsSet("security.rate_limit_fail_closed") {
				failClosed = viper.GetBool("security.rate_limit_fail_closed")
			}
			if failClosed {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "Service Unavailable: rate limiter error"})
				return
			}
		} else if !allowed {
			if s.escalationBroker != nil {
				sessionID := c.GetHeader("X-Loopers-Session-ID")
				if sessionID == "" {
					sessionID = reqID
				}
				req := a2a.EscalationRequest{
					SessionID: sessionID,
					Reason:    "rate_limited",
					AgentName: meta.AgentName,
				}
				resp, escErr := s.escalationBroker.RequestEscalation(c.Request.Context(), req, 5*time.Second)
				if escErr == nil && resp.Approved {
					escalationsApprovedTotal.WithLabelValues("rate_limited").Inc()
					logging.Logger.Info().Str("session_id", sessionID).Msg("rate limit block escalated and approved")
					allowed = true
				} else if escErr != nil {
					escalationsTimeoutTotal.WithLabelValues("rate_limited").Inc()
					logging.Logger.Warn().Err(escErr).Str("session_id", sessionID).Msg("escalation request timed out or failed")
				}
			}

			if !allowed {
				event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
					EventType: "rate_limit_block",
					KeyHash:   keyHash,
					Provider:  providerName,
					Reason:    "rate_limited",
					Detail:    "Rate limit exceeded",
				})
				rateLimitBlocksTotal.WithLabelValues(providerName).Inc()
				requestsTotal.WithLabelValues(providerName, "unknown", "429").Inc()
				c.Header("X-RateLimit-Remaining", "0")
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": "rate limit exceeded",
					"type":  "rate_limit_exceeded",
				})
				return
			} else {
				c.Header("X-RateLimit-Remaining", "0")
			}
		} else {
			c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		}
	}

	prov, err := s.registry.Get(providerName)
	if err != nil {
		requestsTotal.WithLabelValues(providerName, "unknown", "400").Inc()
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	providerKey, _ := c.Get(proxy.ProviderKeyCtx)
	providerKeyStr, _ := providerKey.(string)
	if providerKeyStr == "" {
		if val := os.Getenv(strings.ToUpper(providerName) + "_API_KEY"); val != "" {
			providerKeyStr = val
		} else if val := viper.GetString("providers." + providerName + ".api_key"); val != "" {
			providerKeyStr = val
		}
	}

	// 2. Parse request body for model, max_tokens, and streaming
	bodyBytes, exists := c.Get(RequestBodyCtx)
	if !exists {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "RequestBodyCtx missing - did BodyBuffer run?"})
		return
	}
	body, ok := bodyBytes.([]byte)
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "RequestBodyCtx is not a byte slice"})
		return
	}

	model, isStream, maxTokensVal, mutatedBody, err := prov.ParseRequest(c.Request, body)
	if err != nil {
		requestsTotal.WithLabelValues(providerName, "unknown", "400").Inc()
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON request payload"})
		return
	}

	if override := c.GetHeader("X-Loopers-Model-Override"); override != "" {
		mutatedBody, err = prov.RewriteModel(c.Request, mutatedBody, override)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to apply model override"})
			return
		}
		model = override
	} else if modelMapHeader := c.GetHeader("X-Loopers-Model-Map"); modelMapHeader != "" {
		mappings := strings.Split(modelMapHeader, ",")
		for _, mapping := range mappings {
			parts := strings.SplitN(mapping, "=", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == model {
				alias := strings.TrimSpace(parts[1])
				mutatedBody, err = prov.RewriteModel(c.Request, mutatedBody, alias)
				if err != nil {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to apply model map"})
					return
				}
				model = alias
				break
			}
		}
	}

	if s.otelEnabled {
		span := trace.SpanFromContext(c.Request.Context())
		span.SetAttributes(attribute.String("gen_ai.request.model", model))
	}

	earlySessionID := c.GetHeader("X-Loopers-Session-ID")
	var rawProxyKey string
	if pk, ok := c.Get("ProxyKey"); ok {
		rawProxyKey, _ = pk.(string)
	}
	if rawProxyKey == "" {
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			rawProxyKey = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			rawProxyKey = authHeader
		}
	}

	var promptText string
	var driftScore float64
	var traceCount int

	if earlySessionID != "" && session.IsValidID(earlySessionID) && s.sessionManager != nil {
		if traces, trErr := s.sessionManager.GetSessionTraces(c.Request.Context(), keyHash, earlySessionID); trErr == nil {
			traceCount = len(traces)
		}
	}

	if s.policyEngine != nil {
		// Build a stateful session context for OPA — fetch taint history if a session is active.
		// We do a best-effort early read of the session ID here; the full session enforcement
		// happens later in enforceSessionLimits, but we need the taint context for policy now.
		policySessionCtx := policy.SessionContext{
			ID: earlySessionID,
		}
		var currentState string
		if earlySessionID != "" && session.IsValidID(earlySessionID) && s.sessionManager != nil {
			if taintFlags, tErr := s.sessionManager.GetTaintFlags(c.Request.Context(), keyHash, earlySessionID); tErr == nil {
				policySessionCtx.TaintFlags = taintFlags
			} else {
				logging.Logger.Warn().Err(tErr).Str("session_id", earlySessionID).Msg("proxy_failed_to_fetch_taint_flags")
			}
			if toolHistory, hErr := s.sessionManager.GetToolCallHistory(c.Request.Context(), keyHash, earlySessionID); hErr == nil {
				policySessionCtx.ToolsCalled = toolHistory
			} else {
				logging.Logger.Warn().Err(hErr).Str("session_id", earlySessionID).Msg("proxy_failed_to_fetch_tool_history")
			}
			if traces, trErr := s.sessionManager.GetSessionTraces(c.Request.Context(), keyHash, earlySessionID); trErr == nil {
				policySessionCtx.Traces = traces
			} else {
				logging.Logger.Warn().Err(trErr).Str("session_id", earlySessionID).Msg("proxy_failed_to_fetch_session_traces")
			}

			// Fetch session FSM state if FSM is configured
			if fsm := s.policyEngine.FSM(); fsm != nil {
				if state, sErr := s.sessionManager.GetSessionState(c.Request.Context(), keyHash, earlySessionID, fsm.InitialState); sErr == nil {
					currentState = state
					policySessionCtx.State = state
				} else {
					logging.Logger.Warn().Err(sErr).Str("session_id", earlySessionID).Msg("proxy_failed_to_fetch_session_state")
				}
			}
		}
		if policySessionCtx.TaintFlags == nil {
			policySessionCtx.TaintFlags = make(map[string]bool)
		}
		if policySessionCtx.ToolsCalled == nil {
			policySessionCtx.ToolsCalled = make([]string, 0)
		}
		if policySessionCtx.Traces == nil {
			policySessionCtx.Traces = make([]policy.SessionTrace, 0)
		}

		actionCtx, _ := proxy.MapLLMRequestToContext(providerName, mutatedBody)
		actionCtx.Type = "llm_call"
		actionCtx.Provider = providerName
		if actionCtx.Model == "" {
			actionCtx.Model = model
		}

		if len(actionCtx.ToolCalls) > 0 {
			var maxBR blastradius.BlastRadiusResult
			for _, tc := range actionCtx.ToolCalls {
				br := blastradius.Calculate(tc.Name, tc.Arguments)
				if br.Score > maxBR.Score {
					maxBR = br
				}
			}
			actionCtx.BlastRadius = maxBR.Score
			actionCtx.BlastRadiusTier = maxBR.Tier
			actionCtx.BlastRadiusReasons = maxBR.Reasons
		}

		if earlySessionID != "" && session.IsValidID(earlySessionID) && s.sessionManager != nil && actionCtx.PromptText != "" {
			if driftCtx, dErr := s.sessionManager.ComputeDrift(c.Request.Context(), keyHash, earlySessionID, actionCtx.PromptText, policySessionCtx.Traces); dErr == nil {
				policySessionCtx.Drift = driftCtx
				driftScore = driftCtx.DriftScore
			} else {
				logging.Logger.Warn().Err(dErr).Str("session_id", earlySessionID).Msg("proxy_failed_to_compute_session_drift")
			}
		}
		if actionCtx.PromptText != "" {
			promptText = actionCtx.PromptText
		}

		var agentRiskCtx policy.AgentRiskContext
		if rp != nil {
			agentRiskCtx = policy.AgentRiskContext{
				RiskScore:            rp.RiskScore,
				TotalPolicyBlocks:    rp.TotalPolicyBlocks,
				TotalEscalations:     rp.TotalEscalations,
				TotalSpend:           rp.TotalSpend,
				PersistentTaintFlags: rp.PersistentTaintFlags,
				SessionCount:         rp.SessionCount,
				QuarantineActive:     time.Now().Before(rp.QuarantineUntil),
			}
		}

		decision, err := s.policyEngine.Evaluate(c.Request.Context(), policy.EvalInput{
			Agent: policy.AgentContext{
				KeyHash:   keyHash,
				Name:      meta.Name,
				AgentName: meta.AgentName,
				Owner:     meta.Owner,
				Provider:  meta.Provider,
				Tags:      meta.ParseTags(),
			},
			Request: policy.RequestContext{
				Provider: providerName,
				Model:    model,
				Method:   "llm_call",
				Path:     c.Request.URL.Path,
			},
			Session:   policySessionCtx,
			AgentRisk: agentRiskCtx,
			Action:    actionCtx,
		})
		if err != nil {
			logging.Logger.Error().Err(err).Msg("policy_engine_evaluation_error")
			event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
				EventType: "policy_evaluation_error",
				KeyHash:   keyHash,
				Provider:  providerName,
				Model:     model,
				Reason:    "policy_evaluation_error",
				Detail:    err.Error(),
			})
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "Policy evaluation failed",
				"type":  "internal_error",
			})
			return
		}

		switch decision.Action {
		case "allow":
			// Proceed to trace appending and proxying

		case "deny":
			if rp != nil && s.redis != nil {
				go func() {
					_, _ = riskprofile.UpdateRiskScore(context.Background(), s.redis.GetUnderlyingClient(), keyHash, 10, false, "policy_block")
				}()
			}
			event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
				EventType: "policy_block",
				KeyHash:   keyHash,
				Provider:  providerName,
				Model:     model,
				Reason:    "policy_denied",
				Detail:    decision.Reason,
				RequestID: reqID,
			})
			c.AbortWithStatusJSON(http.StatusForbidden, api.NewPolicyDeniedResponse("", providerName, decision.Reason))
			return

		case "quarantine":
			if rp != nil && s.redis != nil {
				go func() {
					_, _ = riskprofile.UpdateRiskScore(context.Background(), s.redis.GetUnderlyingClient(), keyHash, 25, true, "quarantine")
				}()
			}
			dur, _ := time.ParseDuration(decision.QuarantineFor)
			if dur <= 0 {
				dur = 1 * time.Hour
			}
			_ = s.redis.GetUnderlyingClient().Set(c.Request.Context(), "loopers:quarantine:"+keyHash, "1", dur).Err()
			event.EmitQuarantineEvent(c.Request.Context(), event.QuarantineEvent{
				KeyHash:       keyHash,
				Reason:        decision.Reason,
				QuarantineFor: decision.QuarantineFor,
				Evidence:      decision.Evidence,
				RequestID:     reqID,
			})
			policyQuarantinesTotal.WithLabelValues(providerName, decision.Severity).Inc()
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":    "Request blocked: agent quarantined by security policy",
				"type":     "quarantine",
				"reason":   decision.Reason,
				"duration": decision.QuarantineFor,
			})
			return

		case "escalate":
			if rp != nil && s.redis != nil {
				go func() {
					_, _ = riskprofile.UpdateRiskScore(context.Background(), s.redis.GetUnderlyingClient(), keyHash, 15, false, "escalate")
				}()
			}
			if s.escalationBroker == nil {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
					"error": "Escalation required but broker is not configured",
					"type":  "escalation_unavailable",
				})
				return
			}
			event.EmitEscalationEvent(c.Request.Context(), event.EscalationEvent{
				KeyHash:    keyHash,
				Provider:   providerName,
				SessionID:  earlySessionID,
				Reason:     decision.Reason,
				EscalateTo: decision.EscalateTo,
				Evidence:   decision.Evidence,
				RequestID:  reqID,
			})
			policyEscalationsTotal.WithLabelValues(providerName, decision.EscalateTo).Inc()
			escalationTimeout := 60 * time.Second
			if to := viper.GetDuration("escalation.timeout"); to > 0 {
				escalationTimeout = to
			}
			resp, err := s.escalationBroker.RequestEscalationFromDecision(
				c.Request.Context(), earlySessionID, meta.AgentName, decision, escalationTimeout,
			)
			if err != nil || resp == nil || !resp.Approved {
				escalationsTimeoutTotal.WithLabelValues(decision.Reason).Inc()
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "Request blocked: escalation denied or timed out",
					"type":  "escalation_denied",
				})
				return
			}
			escalationsApprovedTotal.WithLabelValues(decision.Reason).Inc()
			// Approved! Proceed.

		case "transform":
			if len(decision.Transforms) > 0 {
				mutatedBody = applyPromptTransforms(mutatedBody, decision.Transforms)
				for _, tr := range decision.Transforms {
					op := tr.Operation
					if op == "" {
						op = "mask"
					}
					policyTransformsTotal.WithLabelValues(providerName, op).Inc()
				}
			}

		default:
			event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
				EventType: "policy_block",
				KeyHash:   keyHash,
				Provider:  providerName,
				Model:     model,
				Reason:    "unknown_policy_action",
				Detail:    fmt.Sprintf("Unknown action: %s", decision.Action),
				RequestID: reqID,
			})
			c.AbortWithStatusJSON(http.StatusForbidden, api.NewPolicyDeniedResponse("", providerName, decision.Reason))
			return
		}

		promptText = actionCtx.PromptText
		if policySessionCtx.Drift.DriftScore > 0 {
			driftScore = policySessionCtx.Drift.DriftScore
		}
		traceCount = len(policySessionCtx.Traces)

		if earlySessionID != "" && session.IsValidID(earlySessionID) && s.sessionManager != nil {
			truncatedPrompt := actionCtx.PromptText
			if len(truncatedPrompt) > 512 {
				truncatedPrompt = truncatedPrompt[:512]
			}
			_ = s.sessionManager.AppendSessionTrace(c.Request.Context(), keyHash, earlySessionID, policy.SessionTrace{
				Timestamp: time.Now().Unix(),
				Type:      "llm_call",
				Provider:  providerName,
				Model:     model,
				Content:   truncatedPrompt,
			})

			// Handle FSM transitions if configured
			if fsm := s.policyEngine.FSM(); fsm != nil {
				for _, trans := range fsm.Transitions {
					if trans.From == currentState {
						if trans.Trigger == "llm_call" || trans.Trigger == model {
							_ = s.sessionManager.SetSessionState(c.Request.Context(), keyHash, earlySessionID, trans.To)
							break
						}
					}
				}
			}
		}
	}

	if promptText == "" {
		if actCtx, _ := proxy.MapLLMRequestToContext(providerName, mutatedBody); actCtx.PromptText != "" {
			promptText = actCtx.PromptText
		}
	}

	if maxTokensVal == 0 {
		_, _, defaultMaxOut := s.pricing.GetModelPrice(providerName, model)
		maxTokensVal = defaultMaxOut
	}

	// Step 2.2: Cache-Gravity Tier & Effort Routing
	origModel := model
	origInputPrice, origOutputPrice, _ := s.pricing.GetModelPrice(providerName, origModel)
	model, mutatedBody = s.applySmartFinOpsRouting(
		c, providerName, model, mutatedBody, prov, promptText, driftScore, traceCount, &maxTokensVal,
	)

	// Step 2.3: Synthesis-Gated Append-Only Ghost Token Hydration (runs on post-routing model)
	// Only hydrate Ghost Stubs when:
	//   1. The post-routing target model is a Frontier model, AND
	//   2. The current turn requires complex reasoning/synthesis (!isSimpleTurn).
	// On intermediate verification turns (Turn 2) or Fast-Tier turns (Turn 1), the ~80-token
	// Ghost Stub remains in context, preserving ~98% payload compression.
	ghostSessionID := proxy.ResolveGhostSessionID(earlySessionID, keyHash)
	if ghostSessionID != "" && rawProxyKey != "" && s.redis != nil {
		skipHydration := strings.EqualFold(c.GetHeader("X-Loopers-Skip-Hydration"), "true")
		latestUserPrompt := extractLatestUserPrompt(mutatedBody, promptText)
		shouldHydrate := !skipHydration && s.isFrontierModel(providerName, model) && !isSimpleTurn(latestUserPrompt, driftScore)
		if hydrated, hErr := proxy.HydratePromptTail(
			c.Request.Context(),
			keyHash,
			rawProxyKey,
			ghostSessionID,
			mutatedBody,
			s.redis.GetUnderlyingClient(),
			shouldHydrate,
			16000,
		); hErr == nil {
			mutatedBody = hydrated
		}
	}

	// Step 2.4: Deterministic Prefix Canonicalizer (final JSON serialization before token count & signing)
	if canonicalized, cErr := proxy.CanonicalizeRequest(providerName, keyHash, mutatedBody); cErr == nil {
		mutatedBody = canonicalized
	} else {
		logging.Logger.Warn().Err(cErr).Msg("failed_to_canonicalize_request")
	}

	// 3. Pricing lookup
	inputPrice, outputPrice, defaultMaxOut := s.pricing.GetModelPrice(providerName, model)
	if maxTokensVal == 0 {
		maxTokensVal = defaultMaxOut
	}

	// 4. Token counting
	inputTokens, countErr := prov.CountInputTokens(c.Request.Context(), model, mutatedBody, providerKeyStr)
	if countErr != nil {
		requestsTotal.WithLabelValues(providerName, model, "400").Inc()
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Token counting failed: %v", countErr)})
		return
	}

	c.Request.Body = io.NopCloser(bytes.NewReader(mutatedBody))
	c.Request.ContentLength = int64(len(mutatedBody))

	// 5. Estimate cost
	estimatedCost := pricing.EstimateCost(inputTokens, maxTokensVal, inputPrice, outputPrice)

	// 6. Check and reserve budget via Enforcement Engine
	model, estimatedCost, inputPrice, outputPrice, inputTokens, mutatedBody, err = s.enforceBudgetWithFallback(c, providerName, model, estimatedCost, inputPrice, outputPrice, inputTokens, maxTokensVal, mutatedBody, providerKeyStr, keyHash, meta, reqID)
	if err != nil {
		return // response already sent by enforceBudgetWithFallback
	}

	modelPriceDetails, _ := s.pricing.GetModelPriceDetails(providerName, model)
	cacheReadPrice, cacheWritePrice := pricing.ResolveCachePrices(providerName, modelPriceDetails.InputPer1M, modelPriceDetails.CacheReadPer1M, modelPriceDetails.CacheWritePer1M)

	sessionID := c.GetHeader("X-Loopers-Session-ID")
	var sessionBudget float64
	var sessionMaxSteps int

	// 6.5 Session limits and Loop Detection
	if sessionID != "" {
		if s.otelEnabled {
			span := trace.SpanFromContext(c.Request.Context())
			span.SetAttributes(attribute.String("gen_ai.system.session.id", sessionID))
		}

		if !session.IsValidID(sessionID) {
			requestsTotal.WithLabelValues(providerName, model, "400").Inc()
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid session ID format"})
			return
		}

		if s.sessionManager != nil {
			maxSessions := viper.GetInt("session.max_per_key")
			if maxSessions <= 0 {
				maxSessions = 100 // Default to 100 concurrent sessions per key
			}
			allowed, err := s.sessionManager.TrackAndLimitSessions(c.Request.Context(), keyHash, sessionID, maxSessions)
			if err != nil {
				logging.Logger.Error().Err(err).Msg("failed_session_track_limit")
			} else if !allowed {
				requestsTotal.WithLabelValues(providerName, model, "429").Inc()
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": "maximum concurrent sessions per key exceeded",
					"type":  "session_limit_exceeded",
				})
				return
			}
			if s.riskProfileCfg.Enabled && s.redis != nil {
				go func() {
					seenKey := fmt.Sprintf("loopers:session:{%s}:%s:risk_seen", keyHash, sessionID)
					if set, _ := s.redis.GetUnderlyingClient().SetNX(context.Background(), seenKey, "1", 7*24*time.Hour).Result(); set {
						_ = riskprofile.IncrementSessionCount(context.Background(), s.redis.GetUnderlyingClient(), keyHash)
					}
				}()
			}
		}

		if ttlHeader := c.GetHeader("X-Loopers-Session-TTL"); ttlHeader != "" {
			if ttl, err := strconv.Atoi(ttlHeader); err == nil && ttl > 0 {
				maxTtl := viper.GetInt("session.max_ttl_seconds")
				if maxTtl <= 0 {
					maxTtl = 86400
				}
				if ttl > maxTtl {
					ttl = maxTtl
				}
				if s.sessionManager != nil {
					valid, err := s.sessionManager.EnforceAbsoluteTTL(c.Request.Context(), keyHash, sessionID, ttl)
					if err != nil {
						logging.Logger.Error().Err(err).Msg("failed_session_ttl_check")
					} else if !valid {
						requestsTotal.WithLabelValues(providerName, model, "400").Inc()
						c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
							"error": "session expired",
							"type":  "session_ttl_exceeded",
						})
						return
					}
				}
			}
		}

		if budgetHeader := c.GetHeader("X-Loopers-Session-Budget"); budgetHeader != "" {
			allowOverride := viper.GetBool("session.allow_client_budget_override")
			if !viper.IsSet("session.allow_client_budget_override") {
				allowOverride = false // VULN-018: Default to false
			}
			if !allowOverride {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error": "client-controlled session budgets are disabled by policy",
					"type":  "invalid_request_error",
				})
				return
			}
			sessionBudget, _ = strconv.ParseFloat(budgetHeader, 64)
		}
		if stepsHeader := c.GetHeader("X-Loopers-Session-Max-Steps"); stepsHeader != "" {
			allowOverride := viper.GetBool("session.allow_client_budget_override")
			if !viper.IsSet("session.allow_client_budget_override") {
				allowOverride = false // VULN-018: Default to false
			}
			if !allowOverride {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error": "client-controlled session limits are disabled by policy",
					"type":  "invalid_request_error",
				})
				return
			}
			sessionMaxSteps, _ = strconv.Atoi(stepsHeader)
		}

		// Enforce session limits
		err = s.enforceSessionLimits(c, providerName, model, sessionID, sessionBudget, sessionMaxSteps, estimatedCost, keyHash, meta, reqID)
		if err != nil {
			return // response sent
		}

		// Enforce loop detection
		err = s.enforceLoopDetection(c, providerName, model, sessionID, body, estimatedCost, keyHash, meta, reqID)
		if err != nil {
			return // response sent
		}
	}

	// 7. Propagation to context for ReverseProxy
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, startTimeCtx, startTime)
	ctx = context.WithValue(ctx, proxy.ProxyKeyHashCtx, keyHash)
	ctx = context.WithValue(ctx, proxy.RequestCostCtx, estimatedCost)
	ctx = context.WithValue(ctx, proxy.ProviderKeyCtx, providerKeyStr)
	ctx = context.WithValue(ctx, proxy.ProviderCtx, providerName)
	ctx = context.WithValue(ctx, proxy.ProviderInstanceCtx, prov)
	ctx = context.WithValue(ctx, proxy.ModelCtx, model)
	ctx = context.WithValue(ctx, isStreamCtx, isStream)
	ctx = context.WithValue(ctx, inputPriceCtx, inputPrice)
	ctx = context.WithValue(ctx, outputPriceCtx, outputPrice)
	ctx = context.WithValue(ctx, cacheReadPriceCtx, cacheReadPrice)
	ctx = context.WithValue(ctx, cacheWritePriceCtx, cacheWritePrice)
	ctx = context.WithValue(ctx, inputTokensCtx, inputTokens)
	if origModel != model {
		ctx = context.WithValue(ctx, origInputPriceCtx, origInputPrice)
		ctx = context.WithValue(ctx, origOutputPriceCtx, origOutputPrice)
	}
	ctx = context.WithValue(ctx, keyNameCtx, meta.Name)
	if meta.AgentName != "" {
		ctx = context.WithValue(ctx, agentNameCtx, meta.AgentName)
	}
	ctx = context.WithValue(ctx, requestIDCtx, reqID)
	if sessionID != "" {
		ctx = context.WithValue(ctx, sessionIDCtx, sessionID)
		ctx = context.WithValue(ctx, sessionBudgetCtx, sessionBudget)
		ctx = context.WithValue(ctx, sessionMaxStepsCtx, sessionMaxSteps)
	}

	if s.signer != nil && s.signer.Enabled {
		sig, t, err := s.signer.Sign(mutatedBody)
		if err == nil {
			sigHeader := s.signer.FormatHeader(t, sig)
			c.Request.Header.Set("X-Loopers-Signature", sigHeader)
			ctx = context.WithValue(ctx, proxy.RequestSignatureCtx, sigHeader)
		}
	}

	c.Request = c.Request.WithContext(ctx)

	// 8. Stream output using ReverseProxy
	s.proxy.ServeHTTP(c.Writer, c.Request)
}

func (s *Server) modifyResponse(resp *http.Response) error {
	req := resp.Request
	ctx := req.Context()

	sigHeader, _ := ctx.Value(proxy.RequestSignatureCtx).(string)
	if sigHeader != "" {
		resp.Header.Set("X-Loopers-Signature", sigHeader)
	}

	startTime, _ := ctx.Value(startTimeCtx).(time.Time)
	keyHash, _ := ctx.Value(proxy.ProxyKeyHashCtx).(string)
	keyName, _ := ctx.Value(keyNameCtx).(string)
	provName, _ := ctx.Value(proxy.ProviderCtx).(string)
	reservedCost, _ := ctx.Value(proxy.RequestCostCtx).(float64)
	prov, _ := ctx.Value(proxy.ProviderInstanceCtx).(provider.Provider)
	model, _ := ctx.Value(proxy.ModelCtx).(string)
	isStream, _ := ctx.Value(isStreamCtx).(bool)
	inputPrice, _ := ctx.Value(inputPriceCtx).(float64)
	outputPrice, _ := ctx.Value(outputPriceCtx).(float64)
	cacheReadPrice, _ := ctx.Value(cacheReadPriceCtx).(float64)
	cacheWritePrice, _ := ctx.Value(cacheWritePriceCtx).(float64)
	sessionID, _ := ctx.Value(sessionIDCtx).(string)
	reqID, _ := ctx.Value(requestIDCtx).(string)

	if resp.StatusCode != http.StatusOK {
		s.redis.LeaseManager.ReconcileSpend(ctx, keyHash, reservedCost, 0)
		if sessionID != "" {
			sessionSpendKey := fmt.Sprintf("loopers:session:{%s}:%s:spend", keyHash, sessionID)
			if err := s.redis.GetUnderlyingClient().IncrBy(ctx, sessionSpendKey, -budget.ToNano(reservedCost)).Err(); err != nil {
				logging.Logger.Warn().Err(err).Str("session_id", sessionID).Msg("failed to update session spend in redis")
			}
		}

		// Record non-200 request metrics
		latency := time.Since(startTime)
		requestDuration.WithLabelValues(provName).Observe(latency.Seconds())
		requestsTotal.WithLabelValues(provName, model, strconv.Itoa(resp.StatusCode)).Inc()
		return nil
	}

	// Set initial headers for both stream and non-stream
	if !viper.GetBool("server.strip_budget_headers") {
		resp.Header.Set("X-Loopers-Request-Cost-Estimated", fmt.Sprintf("%.6f", reservedCost))
	}
	if agentName, ok := ctx.Value(agentNameCtx).(string); ok && agentName != "" {
		resp.Header.Set("X-Loopers-Agent-Name", agentName)
	}

	if sessionID != "" {
		sessionStepsKey := fmt.Sprintf("loopers:session:{%s}:%s:steps", keyHash, sessionID)

		rdb := s.redis.GetUnderlyingClient()
		vals, _ := rdb.MGet(ctx, sessionStepsKey).Result()
		var stepsVal int64
		if len(vals) == 1 {
			if vals[0] != nil {
				if stVal, err := strconv.ParseInt(fmt.Sprintf("%v", vals[0]), 10, 64); err == nil {
					stepsVal = stVal
				}
			}
		}

		if !viper.GetBool("server.strip_budget_headers") {
			resp.Header.Set("X-Loopers-Session-Steps", fmt.Sprintf("%d", stepsVal))
		}
	}

	if isStream {
		var totalPaid float64 = reservedCost

		resp.Body = proxy.NewSSEStreamReaderWithCache(ctx, resp.Body, prov, inputPrice, outputPrice, cacheReadPrice, cacheWritePrice,
			func(cost float64) bool {
				delta := cost - totalPaid
				if delta <= 0 {
					return true
				}
				if s.redis.LeaseManager.TryAcquireFast(keyHash, delta) {
					totalPaid += delta
					return true
				}
				return false
			},
			func(actualCost, savedUSD float64, inTokens, outTokens int, completion string, forcedCut bool) {
				origIn, _ := ctx.Value(origInputPriceCtx).(float64)
				origOut, _ := ctx.Value(origOutputPriceCtx).(float64)
				savedUSD = computeTotalSavedUSD(actualCost, savedUSD, inTokens, outTokens, origIn, origOut)
				s.redis.LeaseManager.ReconcileSpend(ctx, keyHash, totalPaid, actualCost)
				s.checkBudgetOverdrawAsync(ctx, keyHash, provName, model)
				if savedUSD > 0 {
					savingsUSDTotal.WithLabelValues(provName, keyHash).Add(savedUSD)
					if s.redis != nil {
						_ = s.redis.RecordSavings(context.Background(), keyHash, savedUSD)
					}
				}
				if sessionID != "" {
					sessionSpendKey := fmt.Sprintf("loopers:session:{%s}:%s:spend", keyHash, sessionID)
					if err := s.redis.GetUnderlyingClient().IncrBy(ctx, sessionSpendKey, budget.ToNano(actualCost-totalPaid)).Err(); err != nil {
						logging.Logger.Warn().Err(err).Str("session_id", sessionID).Msg("failed to update session spend in redis")
					}
					if s.sessionManager != nil {
						_ = s.sessionManager.AppendSessionTrace(ctx, keyHash, sessionID, policy.SessionTrace{
							Timestamp: time.Now().Unix(),
							Type:      "llm_response",
							Provider:  provName,
							Model:     model,
							Content:   completion,
						})
					}
				}

				if s.riskProfileCfg.Enabled && s.redis != nil && keyHash != "" && actualCost > 0 {
					go func() {
						_ = riskprofile.AddLifetimeSpend(context.Background(), s.redis.GetUnderlyingClient(), keyHash, actualCost)
					}()
				}

				// Instrument stream metrics upon completion
				latency := time.Since(startTime)
				requestDuration.WithLabelValues(provName).Observe(latency.Seconds())
				requestsTotal.WithLabelValues(provName, model, "200").Inc()
				spendUSDTotal.WithLabelValues(provName, keyHash).Add(actualCost)
				tokensTotal.WithLabelValues(provName, "input").Add(float64(inTokens))
				tokensTotal.WithLabelValues(provName, "output").Add(float64(outTokens))

				// Trigger alerts
				if s.alerter != nil {
					go func() {
						statusMap, err := s.redis.GetBudgetStatus(detachedTraceContext(ctx), keyHash)
						if err == nil {
							limits := make(map[string]float64)
							spends := make(map[string]float64)
							for k, v := range statusMap {
								limits[k] = v.Limit
								spends[k] = v.CurrentSpend
							}
							s.alerter.TriggerThresholdAlerts(detachedTraceContext(ctx), reqID, keyHash, keyName, provName, spends, limits)
						}
					}()
				}
			},
			s.dlpCfg,
			func() {
				dur, _ := time.ParseDuration(s.dlpCfg.QuarantineDuration)
				if dur <= 0 {
					dur = 1 * time.Hour
				}
				_ = s.redis.GetUnderlyingClient().Set(ctx, "loopers:quarantine:"+keyHash, "1", dur).Err()
				if s.riskProfileCfg.Enabled {
					go func() {
						_, _ = riskprofile.UpdateRiskScore(context.Background(), s.redis.GetUnderlyingClient(), keyHash, 30, true, "secret_exfiltration")
					}()
				}
				event.EmitQuarantineEvent(ctx, event.QuarantineEvent{
					KeyHash:       keyHash,
					Reason:        "Secret exfiltration detected in streaming LLM completion",
					QuarantineFor: s.dlpCfg.QuarantineDuration,
					RequestID:     reqID,
				})
				dlpQuarantinesTotal.WithLabelValues(provName).Inc()
			},
		)
	} else {
		limit := viper.GetInt64("server.max_response_bytes")
		if limit == 0 {
			limit = 32 * 1024 * 1024
		}
		var reader io.ReadCloser = resp.Body
		isGzip := resp.Header.Get("Content-Encoding") == "gzip"
		if isGzip {
			gzReader, err := gzip.NewReader(resp.Body)
			if err == nil {
				reader = gzReader
			}
		}
		respBodyBytes, err := io.ReadAll(io.LimitReader(reader, limit))
		if err != nil {
			reader.Close()
			return err
		}
		reader.Close()

		if isGzip {
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
		}

		// Outbound DLP Gate (Capability 4: Non-streaming LLM completion)
		if s.dlpCfg.Enabled {
			dlpResult, dlpBody, dlpErr := inspector.InspectJSONCompletion(respBodyBytes, provName, s.dlpCfg)
			if dlpErr != nil {
				logging.Logger.Error().Err(dlpErr).Msg("Failed to inspect/marshal completion payload for DLP")
			} else if dlpResult.Action != "allow" {
				switch dlpResult.Action {
				case "quarantine":
					dur, _ := time.ParseDuration(s.dlpCfg.QuarantineDuration)
					if dur <= 0 {
						dur = 1 * time.Hour
					}
					_ = s.redis.GetUnderlyingClient().Set(ctx, "loopers:quarantine:"+keyHash, "1", dur).Err()
					if s.riskProfileCfg.Enabled {
						go func() {
							_, _ = riskprofile.UpdateRiskScore(context.Background(), s.redis.GetUnderlyingClient(), keyHash, 30, true, "secret_exfiltration")
						}()
					}
					event.EmitQuarantineEvent(ctx, event.QuarantineEvent{
						KeyHash:       keyHash,
						Reason:        dlpResult.Reason,
						QuarantineFor: s.dlpCfg.QuarantineDuration,
						Evidence:      dlpResult.Matches,
						RequestID:     reqID,
					})
					dlpQuarantinesTotal.WithLabelValues(provName).Inc()

					denialBody := []byte(`{"error":"completion blocked by security policy","type":"dlp_quarantine"}`)
					resp.StatusCode = http.StatusForbidden
					resp.Body = io.NopCloser(bytes.NewReader(denialBody))
					resp.ContentLength = int64(len(denialBody))
					resp.Header.Set("Content-Length", strconv.Itoa(len(denialBody)))
					resp.Header.Set("X-Loopers-DLP-Block", "true")
					s.redis.LeaseManager.ReconcileSpend(ctx, keyHash, reservedCost, 0)
					return nil

				case "mask":
					respBodyBytes = dlpBody
					resp.Header.Set("X-Loopers-DLP-Redacted", "true")
					event.EmitBlockEvent(ctx, event.BlockEvent{
						EventType: "dlp_redaction",
						KeyHash:   keyHash,
						Provider:  provName,
						Model:     model,
						Reason:    dlpResult.Reason,
						Detail:    "Sensitive content masked by outbound DLP policy",
						RequestID: reqID,
					})
					dlpRedactionsTotal.WithLabelValues(provName).Inc()
				}
			}
		}

		resp.Body = io.NopCloser(bytes.NewReader(respBodyBytes))
		resp.ContentLength = int64(len(respBodyBytes))
		resp.Header.Set("Content-Length", strconv.Itoa(len(respBodyBytes)))

		var totalInputTokens int
		var totalOutputTokens int
		var cacheReadTokens int
		var cacheWriteTokens int

		var completion string
		if prov != nil {
			if cp, ok := prov.(provider.CacheAwareProvider); ok {
				usage, _ := cp.ParseNonStreamUsage(respBodyBytes)
				totalInputTokens = usage.InputTokens
				totalOutputTokens = usage.OutputTokens
				cacheReadTokens = usage.CacheReadTokens
				cacheWriteTokens = usage.CacheCreationTokens
			} else {
				totalInputTokens, totalOutputTokens, _ = prov.ParseNonStreamResponse(respBodyBytes)
			}
			if compl, err := proxy.MapLLMResponse(provName, respBodyBytes); err == nil {
				completion = compl
				if len(completion) > 512 {
					completion = completion[:512]
				}
			}
		}

		actualCost, savedUSD := pricing.ActualCostWithCache(totalInputTokens, totalOutputTokens, cacheReadTokens, cacheWriteTokens, inputPrice, outputPrice, cacheReadPrice, cacheWritePrice)
		origIn, _ := ctx.Value(origInputPriceCtx).(float64)
		origOut, _ := ctx.Value(origOutputPriceCtx).(float64)
		savedUSD = computeTotalSavedUSD(actualCost, savedUSD, totalInputTokens+cacheReadTokens+cacheWriteTokens, totalOutputTokens, origIn, origOut)
		s.redis.LeaseManager.ReconcileSpend(ctx, keyHash, reservedCost, actualCost)
		s.checkBudgetOverdrawAsync(ctx, keyHash, provName, model)
		if !viper.GetBool("server.strip_budget_headers") {
			resp.Header.Set("X-Loopers-Request-Cost", fmt.Sprintf("%.6f", actualCost))
			if savedUSD > 0 {
				resp.Header.Set("X-Loopers-Saved-USD", fmt.Sprintf("%.6f", savedUSD))
			}
		}
		if savedUSD > 0 {
			savingsUSDTotal.WithLabelValues(provName, keyHash).Add(savedUSD)
			if s.redis != nil {
				_ = s.redis.RecordSavings(context.Background(), keyHash, savedUSD)
			}
		}

		if sessionID != "" {
			sessionSpendKey := fmt.Sprintf("loopers:session:{%s}:%s:spend", keyHash, sessionID)
			sessionStepsKey := fmt.Sprintf("loopers:session:{%s}:%s:steps", keyHash, sessionID)

			if err := s.redis.GetUnderlyingClient().IncrBy(ctx, sessionSpendKey, budget.ToNano(actualCost-reservedCost)).Err(); err != nil {
				logging.Logger.Warn().Err(err).Str("session_id", sessionID).Msg("failed to update session spend in redis")
			}

			if s.sessionManager != nil {
				_ = s.sessionManager.AppendSessionTrace(ctx, keyHash, sessionID, policy.SessionTrace{
					Timestamp: time.Now().Unix(),
					Type:      "llm_response",
					Provider:  provName,
					Model:     model,
					Content:   completion,
				})
			}

			rdb := s.redis.GetUnderlyingClient()
			vals, _ := rdb.MGet(ctx, sessionStepsKey).Result()
			var stepsVal int64
			if len(vals) == 1 {
				if vals[0] != nil {
					if stVal, err := strconv.ParseInt(fmt.Sprintf("%v", vals[0]), 10, 64); err == nil {
						stepsVal = stVal
					}
				}
			}

			if !viper.GetBool("server.strip_budget_headers") {
				resp.Header.Set("X-Loopers-Session-Steps", fmt.Sprintf("%d", stepsVal))
			}
		}

		if s.riskProfileCfg.Enabled && s.redis != nil && keyHash != "" && actualCost > 0 {
			go func() {
				_ = riskprofile.AddLifetimeSpend(context.Background(), s.redis.GetUnderlyingClient(), keyHash, actualCost)
			}()
		}

		// Record non-stream metrics
		latency := time.Since(startTime)
		requestDuration.WithLabelValues(provName).Observe(latency.Seconds())
		requestsTotal.WithLabelValues(provName, model, "200").Inc()
		spendUSDTotal.WithLabelValues(provName, keyHash).Add(actualCost)
		tokensTotal.WithLabelValues(provName, "input").Add(float64(totalInputTokens))
		tokensTotal.WithLabelValues(provName, "output").Add(float64(totalOutputTokens))

		// Trigger alerts
		if s.alerter != nil {
			go func() {
				statusMap, err := s.redis.GetBudgetStatus(detachedTraceContext(ctx), keyHash)
				if err == nil {
					limits := make(map[string]float64)
					spends := make(map[string]float64)
					for k, v := range statusMap {
						limits[k] = v.Limit
						spends[k] = v.CurrentSpend
					}
					s.alerter.TriggerThresholdAlerts(detachedTraceContext(ctx), reqID, keyHash, keyName, provName, spends, limits)
				}
			}()
		}
	}

	return nil
}

func (s *Server) checkBudgetOverdrawAsync(ctx context.Context, keyHash, providerName, model string) {
	go func() {
		detached := detachedTraceContext(ctx)
		statusMap, err := s.redis.GetBudgetStatus(detached, keyHash)
		if err != nil {
			logging.Logger.Warn().Err(err).Msg("failed to check budget status async")
			return
		}

		for window, status := range statusMap {
			if status.Limit > 0 && status.CurrentSpend > status.Limit {
				event.EmitBlockEvent(detached, event.BlockEvent{
					EventType: "budget_overdrawn",
					KeyHash:   keyHash,
					Provider:  providerName,
					Model:     model,
					Reason:    "burst_overdraw",
					Detail:    fmt.Sprintf("Budget overdrawn in %s window: spend %.6f exceeded limit %.6f", window, status.CurrentSpend, status.Limit),
				})
				// Only emit one alert per check
				break
			}
		}
	}()
}

func applyPromptTransforms(body []byte, transforms []policy.TransformRule) []byte {
	if len(transforms) == 0 {
		return body
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return body
	}
	mutated := false
	for _, tr := range transforms {
		// Handle top-level "prompt" field (non-chat completions format)
		if tr.Field == "prompt_text" || tr.Field == "prompt" {
			if _, ok := data["prompt"].(string); ok {
				switch tr.Operation {
				case "mask":
					data["prompt"] = "***"
					mutated = true
				case "redact":
					delete(data, "prompt")
					mutated = true
				}
			}
		}
		// Handle chat completions messages array
		// When field is prompt_text/prompt, mask the "content" key inside each message.
		// When field is another key (e.g. "role"), mask that specific key.
		if msgs, ok := data["messages"].([]interface{}); ok {
			targetKey := tr.Field
			if tr.Field == "prompt_text" || tr.Field == "prompt" {
				targetKey = "content"
			}
			for _, m := range msgs {
				if msgMap, ok := m.(map[string]interface{}); ok {
					switch tr.Operation {
					case "mask":
						if _, hasField := msgMap[targetKey]; hasField {
							msgMap[targetKey] = "***"
							mutated = true
						}
					case "redact":
						if _, hasField := msgMap[targetKey]; hasField {
							delete(msgMap, targetKey)
							mutated = true
						}
					}
				}
			}
		}
	}
	if mutated {
		if updated, err := json.Marshal(data); err == nil {
			return updated
		}
	}
	return body
}

func (s *Server) isFrontierModel(providerName, model string) bool {
	if s.pricing != nil {
		return s.pricing.IsFrontierModel(providerName, model)
	}
	m := strings.ToLower(model)
	if strings.Contains(m, "mini") || strings.Contains(m, "nano") ||
		strings.Contains(m, "haiku") || strings.Contains(m, "flash") ||
		strings.Contains(m, "small") {
		return false
	}
	return strings.Contains(m, "gpt-5") || strings.Contains(m, "gpt-4") ||
		strings.Contains(m, "claude-") || strings.Contains(m, "o1") ||
		strings.Contains(m, "o3") || strings.Contains(m, "pro") ||
		strings.Contains(m, "large") || strings.Contains(m, "reasoner")
}

func extractLatestUserPrompt(body []byte, fallback string) string {
	var reqMap map[string]interface{}
	if err := json.Unmarshal(body, &reqMap); err == nil {
		if msgs, ok := reqMap["messages"].([]interface{}); ok {
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgMap, ok := msgs[i].(map[string]interface{}); ok {
					if role, _ := msgMap["role"].(string); role == "user" {
						if content, ok := msgMap["content"].(string); ok && content != "" {
							return content
						}
					}
				}
			}
		}
	}
	return fallback
}

func isSimpleTurn(prompt string, driftScore float64) bool {
	if len(prompt) == 0 || len(prompt) > 600 {
		return false
	}
	if driftScore >= 0.20 {
		return false
	}
	lower := strings.ToLower(prompt)
	complexMarkers := []string{"architect", "prove", "step by step", "root cause", "security audit", "synthesize", "synthesis"}
	for _, marker := range complexMarkers {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func (s *Server) applySmartFinOpsRouting(
	c *gin.Context,
	providerName string,
	model string,
	mutatedBody []byte,
	prov provider.Provider,
	promptText string,
	driftScore float64,
	traceCount int,
	maxTokensVal *int,
) (string, []byte) {
	if prov == nil || strings.EqualFold(c.GetHeader("X-Loopers-Force-Frontier"), "true") {
		return model, mutatedBody
	}

	userMsgCount := 0
	turnPrompt := promptText
	var reqMap map[string]interface{}
	if err := json.Unmarshal(mutatedBody, &reqMap); err == nil {
		if msgs, ok := reqMap["messages"].([]interface{}); ok {
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgMap, ok := msgs[i].(map[string]interface{}); ok {
					if role, _ := msgMap["role"].(string); role == "user" {
						userMsgCount++
						if turnPrompt == promptText {
							if content, ok := msgMap["content"].(string); ok && content != "" {
								turnPrompt = content
							}
						}
					}
				}
			}
		}
	}

	isSimple := isSimpleTurn(turnPrompt, driftScore)

	// A session has a warm multi-turn cache only after the first full user turn completes
	// (userMsgCount > 1 or traceCount > 1). Keeping all tool-call sub-steps of Turn 1
	// (where userMsgCount == 1) on the same Fast-Tier model avoids mid-turn model switching.
	isWarmCache := traceCount > 1 || userMsgCount > 1

	// Case A: Cold Cache (Turn 1 / traceCount <= 1 && userMsgCount <= 1)
	if !isWarmCache {
		if isSimple {
			fastTier := s.pricing.GetFastTier(providerName, model)
			if fastTier != "" && fastTier != model {
				if rewritten, err := prov.RewriteModel(c.Request, mutatedBody, fastTier); err == nil {
					c.Writer.Header().Set("X-Loopers-FinOps-Route", "fast-tier")
					return fastTier, rewritten
				}
			}
		}
	} else {
		// Case B: Warm Cache (traceCount > 1 || msgCount > 2)
		// Model is pinned to preserve the 90% KV cache read discount.
		if isSimple && *maxTokensVal > 1024 {
			*maxTokensVal = 1024
			if reqMap != nil {
				if _, hasExplicit := reqMap["max_tokens"]; !hasExplicit {
					reqMap["max_tokens"] = 1024
					if updated, err := json.Marshal(reqMap); err == nil {
						mutatedBody = updated
					}
				}
			}
			c.Writer.Header().Set("X-Loopers-FinOps-Throttled", "true")
		}
	}

	return model, mutatedBody
}

func computeTotalSavedUSD(actualCost, cacheSavedUSD float64, totalPromptTokens, totalOutputTokens int, origInPrice, origOutPrice float64) float64 {
	saved := cacheSavedUSD
	if origInPrice > 0 || origOutPrice > 0 {
		origCost := (float64(totalPromptTokens)*origInPrice + float64(totalOutputTokens)*origOutPrice) / 1e6
		if origCost > actualCost {
			// Counterfactual cost against the original frontier model requested by the client
			routeSaved := origCost - actualCost
			if routeSaved > saved {
				saved = routeSaved
			}
		}
	}
	return saved
}
