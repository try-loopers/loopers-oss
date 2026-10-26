package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/try-loopers/loopers-oss/internal/a2a"
	"github.com/try-loopers/loopers-oss/internal/alerting"
	"github.com/try-loopers/loopers-oss/internal/blastradius"
	"github.com/try-loopers/loopers-oss/internal/budget"
	"github.com/try-loopers/loopers-oss/internal/event"
	"github.com/try-loopers/loopers-oss/internal/inspector"
	"github.com/try-loopers/loopers-oss/internal/keyring"
	"github.com/try-loopers/loopers-oss/internal/logging"
	"github.com/try-loopers/loopers-oss/internal/netutil"
	"github.com/try-loopers/loopers-oss/internal/policy"
	"github.com/try-loopers/loopers-oss/internal/pricing"
	proxyPkg "github.com/try-loopers/loopers-oss/internal/proxy"
	"github.com/try-loopers/loopers-oss/internal/riskprofile"
	"github.com/try-loopers/loopers-oss/internal/session"
	"github.com/try-loopers/loopers-oss/internal/signature"
	"github.com/try-loopers/loopers-oss/pkg/api"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type mcpCtxKey string

const (
	mcpSessionIDCtxKey      mcpCtxKey = "mcpSessionID"
	mcpGhostSessionIDCtxKey mcpCtxKey = "mcpGhostSessionID"
	mcpProxyKeyCtxKey       mcpCtxKey = "mcpProxyKey"
	mcpServerNameCtxKey mcpCtxKey = "mcpServerName"
	mcpToolNameCtxKey   mcpCtxKey = "mcpToolName"
	mcpMethodCtxKey     mcpCtxKey = "mcpMethod"
	mcpServerCtxKey     mcpCtxKey = "mcpServer"
	mcpMemoKeyCtxKey    mcpCtxKey = "mcpMemoKey"
)

// sensitiveTaintTools is the set of tool-name substrings that auto-set the
// "secret_accessed" taint flag on a session when called.
// Operators can extend this list via the viper config key "policy.taint_tool_patterns".
var defaultSensitiveTaintPatterns = []string{
	"read_secret",
	"get_credentials",
	"fetch_api_key",
	"database_query",
	"get_secret",
	"retrieve_secret",
	"kv_get",
	"vault_read",
}

// isSensitiveTaintTool returns true if the tool name matches any known sensitive pattern.
func isSensitiveTaintTool(toolName string) bool {
	lower := strings.ToLower(toolName)
	patterns := viper.GetStringSlice("policy.taint_tool_patterns")
	if len(patterns) == 0 {
		patterns = defaultSensitiveTaintPatterns
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

type Handler struct {
	cfg              Config
	riskProfileCfg   riskprofile.Config
	budgetClient     *budget.Client
	pricingStore     *pricing.Store
	alerter          *alerting.Alerter
	circuitBreaker   *CircuitBreaker
	sessionManager   *session.Manager
	policyEngine     *policy.Engine
	signer           *signature.Signer
	escalationBroker *a2a.EscalationBroker
	proxy            *Proxy
	servers          map[string]string
	allowedMethods   map[string]bool
}

func (h *Handler) SetEscalationBroker(broker *a2a.EscalationBroker) {
	h.escalationBroker = broker
}

func (h *Handler) SetRiskProfileConfig(cfg riskprofile.Config) {
	h.riskProfileCfg = cfg
}

func NewHandler(cfg Config, riskProfileCfg riskprofile.Config, budgetClient *budget.Client, pricingStore *pricing.Store, alerter *alerting.Alerter, sessionManager *session.Manager, policyEngine *policy.Engine, signer *signature.Signer) *Handler {
	cb := NewCircuitBreaker(cfg.CircuitBreaker, budgetClient.GetUnderlyingClient())

	servers := make(map[string]string)
	for _, srv := range cfg.Servers {
		if netutil.IsPrivateURL(srv.URL) && !viper.GetBool("testing.allow_private_urls") {
			logging.Logger.Fatal().Str("mcp_server", srv.Name).Str("url", srv.URL).Msg("MCP server URL points to a private/internal IP address (SSRF protection).")
		}
		servers[srv.Name] = srv.URL
	}

	allowedMethods := make(map[string]bool)
	if len(cfg.AllowedMethods) == 0 {
		cfg.AllowedMethods = []string{"tools/list", "tools/call", "initialize", "ping", "notifications/initialized"}
	}
	for _, m := range cfg.AllowedMethods {
		allowedMethods[m] = true
	}

	h := &Handler{
		cfg:            cfg,
		riskProfileCfg: riskProfileCfg,
		budgetClient:   budgetClient,
		pricingStore:   pricingStore,
		alerter:        alerter,
		circuitBreaker: cb,
		sessionManager: sessionManager,
		policyEngine:   policyEngine,
		signer:         signer,
		servers:        servers,
		allowedMethods: allowedMethods,
	}

	h.proxy = NewProxy(h.modifyResponse)
	return h
}

func (h *Handler) modifyResponse(resp *http.Response) error {
	req := resp.Request
	ctx := req.Context()

	sigHeader, _ := ctx.Value(proxyPkg.RequestSignatureCtx).(string)
	if sigHeader != "" {
		resp.Header.Set("X-Loopers-Signature", sigHeader)
	}

	mcpMethod, _ := ctx.Value(mcpMethodCtxKey).(string)
	serverName, _ := ctx.Value(mcpServerCtxKey).(string)
	if mcpMethod == "tools/list" && resp.StatusCode == http.StatusOK {
		limit := viper.GetInt64("server.max_response_bytes")
		if limit == 0 {
			limit = 32 * 1024 * 1024
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
		resp.Body.Close()
		if err == nil {
			sanitized, _ := SanitizeToolList(body, h.cfg.Sanitizer, serverName)
			resp.Body = io.NopCloser(bytes.NewReader(sanitized))
			resp.ContentLength = int64(len(sanitized))
			resp.Header.Set("Content-Length", strconv.Itoa(len(sanitized)))
		} else {
			resp.Body = io.NopCloser(bytes.NewReader(body))
		}
	}

	reservedCost, hasCost := ctx.Value(proxyPkg.RequestCostCtx).(float64)
	keyHash, hasKey := ctx.Value(proxyPkg.ProxyKeyHashCtx).(string)

	// Tool Response Inspection & Read Memoization
	if mcpMethod == "tools/call" && resp.StatusCode == http.StatusOK && hasKey {
		limit := viper.GetInt64("server.max_response_bytes")
		if limit == 0 {
			limit = 10 * 1024 * 1024 // 10MB default
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
		resp.Body.Close()
		if err == nil {
			allowed := true
			if h.cfg.Inspector.Enabled {
				result := inspector.InspectToolResponse(body, h.cfg.Inspector.CustomInjectionPatterns)
				switch result.Action {
				case "quarantine":
					allowed = false
					dur, _ := time.ParseDuration(h.cfg.Inspector.QuarantineDuration)
					if dur <= 0 {
						dur = 1 * time.Hour
					}
					_ = h.budgetClient.GetUnderlyingClient().Set(req.Context(), "loopers:quarantine:"+keyHash, "1", dur).Err()
					if h.riskProfileCfg.Enabled {
						go func() {
							_, _ = riskprofile.UpdateRiskScore(context.Background(), h.budgetClient.GetUnderlyingClient(), keyHash, 30, true, "secret_exfiltration")
						}()
					}
					event.EmitQuarantineEvent(req.Context(), event.QuarantineEvent{
						KeyHash:       keyHash,
						Reason:        result.Reason,
						QuarantineFor: h.cfg.Inspector.QuarantineDuration,
					})
					toolName, _ := ctx.Value(mcpToolNameCtxKey).(string)
					denial, _ := json.Marshal(api.NewMCPPolicyDeniedResponse(jsonRPCIDFromBody(body), toolName, result.Reason))
					resp.Body = io.NopCloser(bytes.NewReader(denial))
					resp.ContentLength = int64(len(denial))
					resp.Header.Set("Content-Length", strconv.Itoa(len(denial)))
					resp.Header.Set("X-Loopers-Policy-Block", "true")
					resp.Header.Set("X-Loopers-Block-Reason", result.Reason)
				case "transform":
					allowed = false
					event.EmitBlockEvent(req.Context(), event.BlockEvent{
						EventType: "response_injection_redacted",
						KeyHash:   keyHash,
						Provider:  serverName,
						Reason:    result.Reason,
						Detail:    "Malicious content redacted from tool response",
					})
					resp.Body = io.NopCloser(bytes.NewReader(result.NewBody))
					resp.ContentLength = int64(len(result.NewBody))
					resp.Header.Set("Content-Length", strconv.Itoa(len(result.NewBody)))
					resp.Header.Set("X-Loopers-Response-Redacted", "true")
				default:
					// allow - restore body
					resp.Body = io.NopCloser(bytes.NewReader(body))
				}
			} else {
				resp.Body = io.NopCloser(bytes.NewReader(body))
			}

			if allowed {
				sessionID, _ := ctx.Value(mcpSessionIDCtxKey).(string)
				ghostSessionID, _ := ctx.Value(mcpGhostSessionIDCtxKey).(string)
				if ghostSessionID == "" {
					ghostSessionID = sessionID
				}
				proxyKey, _ := ctx.Value(mcpProxyKeyCtxKey).(string)
				if proxyKey == "" {
					proxyKey, _ = ctx.Value(proxyPkg.RawProxyKeyCtx).(string)
				}
				toolName, _ := ctx.Value(mcpToolNameCtxKey).(string)

				// Ghost Tokens Offloading: compress and encrypt large tool results
				if h.cfg.GhostTokens.Enabled && h.budgetClient != nil && h.budgetClient.GetUnderlyingClient() != nil && ghostSessionID != "" && proxyKey != "" {
					thresholdBytes := int(h.cfg.GhostTokens.ThresholdBytes)
					if thresholdBytes <= 0 {
						thresholdBytes = 8192
					}
					ttl := h.cfg.GhostTokens.TTLSeconds
					if ttl <= 0 {
						ttl = 1800
					}

					if len(body) > thresholdBytes {
						var rpcMap map[string]interface{}
						if jsonErr := json.Unmarshal(body, &rpcMap); jsonErr == nil {
							if resVal, hasRes := rpcMap["result"]; hasRes {
								var rawPayload []byte
								isContentText := false
								var contentList []interface{}
								var firstItem map[string]interface{}

								if resMap, ok := resVal.(map[string]interface{}); ok {
									if cList, ok := resMap["content"].([]interface{}); ok && len(cList) > 0 {
										if itemMap, ok := cList[0].(map[string]interface{}); ok {
											if text, ok := itemMap["text"].(string); ok && len(text) > thresholdBytes {
												rawPayload = []byte(text)
												isContentText = true
												contentList = cList
												firstItem = itemMap
											}
										}
									}
								}

								if len(rawPayload) == 0 && len(body) > thresholdBytes {
									rawPayload, _ = json.Marshal(resVal)
								}

								if len(rawPayload) > 0 {
									blobID := proxyPkg.ContentBlobID(rawPayload)
									ghostKey := proxyPkg.DeriveGhostKey(proxyKey, ghostSessionID)
									enc, encErr := proxyPkg.EncryptGhostBlob(ghostKey, rawPayload)
									if encErr == nil {
										rdbKey := fmt.Sprintf("loopers:blob:{%s}:%s:%s", keyHash, ghostSessionID, blobID)
										if setErr := h.budgetClient.GetUnderlyingClient().Set(req.Context(), rdbKey, enc, time.Duration(ttl)*time.Second).Err(); setErr == nil {
											stubBytes, stubErr := proxyPkg.CreateGhostStub(toolName, rawPayload, blobID)
											if stubErr == nil {
												if isContentText {
													firstItem["text"] = string(stubBytes)
													contentList[0] = firstItem
													resMap := resVal.(map[string]interface{})
													resMap["content"] = contentList
													rpcMap["result"] = resMap
												} else {
													var stubObj interface{}
													_ = json.Unmarshal(stubBytes, &stubObj)
													rpcMap["result"] = stubObj
												}
												if newBodyBytes, mErr := json.Marshal(rpcMap); mErr == nil {
													body = newBodyBytes
													resp.Body = io.NopCloser(bytes.NewReader(body))
													resp.ContentLength = int64(len(body))
													resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
													resp.Header.Set("X-Loopers-Ghost-Token", blobID)
												}
											}
										}
									}
								}
							}
						}
					}
				}

				if memoKey, ok := ctx.Value(mcpMemoKeyCtxKey).(string); ok && memoKey != "" && h.budgetClient != nil {
					window := h.cfg.CircuitBreaker.WindowSeconds
					if window <= 0 {
						window = 60
					}
					_ = h.budgetClient.GetUnderlyingClient().Set(req.Context(), memoKey, body, time.Duration(window)*time.Second).Err()
				}
			}
		} else {
			resp.Body = io.NopCloser(bytes.NewReader([]byte{}))
		}
	}

	if hasCost && hasKey {
		if resp.StatusCode != http.StatusOK {
			// Refund cost on failure
			h.budgetClient.LeaseManager.ReconcileSpend(ctx, keyHash, reservedCost, 0)
			return nil
		}

		// Tool call successful, commit the spend
		h.budgetClient.LeaseManager.ReconcileSpend(ctx, keyHash, reservedCost, reservedCost)
		resp.Header.Set("X-Loopers-Tool-Cost", fmt.Sprintf("%.6f", reservedCost))
	}

	return nil
}

// abortWithMCPPolicyDenied writes a JSON-RPC 2.0 error response body at HTTP 200.
// This allows MCP client libraries (LangChain, AutoGen, etc.) to surface the denial
// as a tool failure message in the LLM's context rather than crashing on a 403.
func abortWithMCPPolicyDenied(c *gin.Context, req *JSONRPCRequest, toolName, reason string) {
	var reqID any
	if req != nil {
		reqID = req.ID
	}
	resp := api.NewMCPPolicyDeniedResponse(reqID, toolName, reason)
	c.Header("X-Loopers-Policy-Block", "true")
	c.Header("X-Loopers-Block-Reason", reason)
	c.JSON(http.StatusOK, resp)
	c.Abort()
}

func (h *Handler) HandleMCP(c *gin.Context) {
	serverName := c.Param("server")
	targetURL, ok := h.servers[serverName]
	if !ok {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("MCP server '%s' not configured", serverName)})
		return
	}
	if netutil.IsPrivateURL(targetURL) && !viper.GetBool("testing.allow_private_urls") {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "MCP server URL resolves to a private IP (SSRF protection)"})
		return
	}

	// Auth check using the same dual-mode system (JWT or static key)
	var meta *keyring.KeyMetadata
	var keyHash string

	if claims, exists := c.Get("JWTClaims"); exists {
		meta = claims.(*keyring.KeyMetadata)
		if keyHashCtx, ok := c.Get("KeyHash"); ok {
			keyHash = keyHashCtx.(string)
		} else {
			keyHash = keyring.HashKey(meta.Name)
		}

		dpopHeader := c.GetHeader("DPoP")
		if dpopHeader != "" {
			requestURL := "https://" + c.Request.Host + c.Request.URL.Path
			if pub := viper.GetString("server.public_url"); pub != "" {
				requestURL = strings.TrimSuffix(pub, "/") + c.Request.URL.Path
			}
			_, err := keyring.ValidateDPoPAndReplay(c.Request.Context(), h.budgetClient.GetUnderlyingClient(), dpopHeader, c.Request.Method, requestURL, meta.Jkt)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
				return
			}
		} else if meta.Jkt != "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing DPoP header"})
			return
		}

	} else if keyHashCtx, exists := c.Get("KeyHash"); exists {
		keyHash = keyHashCtx.(string)
		var err error
		meta, err = keyring.GetKeyMetadata(c.Request.Context(), h.budgetClient.GetUnderlyingClient(), keyHash)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: key not registered"})
			return
		}
	} else {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing Authorization header"})
		return
	}

	if !strings.EqualFold(meta.Active, "true") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: key has been revoked"})
		return
	}

	// Quarantine Check: enforce at auth layer before any policy evaluation
	quarantineKey := "loopers:quarantine:" + keyHash
	if exists, _ := h.budgetClient.GetUnderlyingClient().Exists(c.Request.Context(), quarantineKey).Result(); exists > 0 {
		event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
			EventType: "quarantine_active",
			KeyHash:   keyHash,
			Provider:  serverName,
			Reason:    "quarantine_active",
			Detail:    "Agent is under active policy quarantine",
		})
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "Agent is currently quarantined by security policy",
			"type":  "quarantine_active",
		})
		return
	}

	// Risk Profile Check: enforce behavioral thresholds before any policy evaluation
	var rp *riskprofile.AgentRiskProfile
	if h.riskProfileCfg.Enabled && h.budgetClient != nil {
		loaded, rpErr := riskprofile.GetProfile(c.Request.Context(), h.budgetClient.GetUnderlyingClient(), keyHash)
		if rpErr == nil && loaded != nil {
			rp = loaded
			if rp.RiskScore > h.riskProfileCfg.PermanentBlockThreshold {
				event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
					EventType: "agent_risk_blocked",
					KeyHash:   keyHash,
					Provider:  serverName,
					Reason:    "agent_risk_blocked",
					Detail:    fmt.Sprintf("Agent risk score %d exceeds permanent block threshold %d", rp.RiskScore, h.riskProfileCfg.PermanentBlockThreshold),
				})
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "Agent permanently blocked due to high behavioral risk",
					"type":  "agent_risk_blocked",
				})
				return
			}
			if rp.RiskScore > h.riskProfileCfg.AutoQuarantineThreshold {
				dur := 1 * time.Hour
				_ = h.budgetClient.GetUnderlyingClient().Set(c.Request.Context(), "loopers:quarantine:"+keyHash, "1", dur).Err()
				event.EmitQuarantineEvent(c.Request.Context(), event.QuarantineEvent{
					KeyHash:       keyHash,
					Reason:        fmt.Sprintf("Agent risk score %d exceeded auto-quarantine threshold %d", rp.RiskScore, h.riskProfileCfg.AutoQuarantineThreshold),
					QuarantineFor: "1h",
				})
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "Agent is currently quarantined by security policy",
					"type":  "quarantine_active",
				})
				return
			}
		}
	}

	// Parse request body for JSON-RPC
	// The body is available via the BodyBuffer middleware context key
	// Wait, internal/server/server.go uses RequestBodyCtx which is a constant in server package.
	// We might need to export it or fetch from context by string. It's usually "RequestBody".
	bodyBytes, exists := c.Get("RequestBody")
	if !exists {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "RequestBody missing"})
		return
	}
	body, _ := bodyBytes.([]byte)

	if h.cfg.MaxRequestSize > 0 && int64(len(body)) > h.cfg.MaxRequestSize {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "MCP request body exceeds maximum allowed size"})
		return
	}

	req, err := ParseJSONRPC(body)
	if err != nil || req == nil {
		// Pass-through transparently if not JSON-RPC
		h.signRequest(c, body)
		h.forward(c, targetURL, nil, 0)
		return
	}

	if !h.allowedMethods[req.Method] {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": fmt.Sprintf("MCP method '%s' is not allowed", req.Method),
			"type":  "mcp_method_not_allowed",
		})
		return
	}

	if req.Method != "tools/call" {
		if req.Method == "tools/list" {
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), mcpMethodCtxKey, "tools/list"))
		} else {
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), mcpMethodCtxKey, req.Method))
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), mcpServerCtxKey, serverName))
		// Pass-through transparently for allowed non-tools/call methods
		h.signRequest(c, body)
		h.forward(c, targetURL, nil, 0)
		return
	}

	toolParams, err := ParseToolCallParams(req.Params)
	if err != nil || toolParams == nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid tools/call params"})
		return
	}

	var toolArgs map[string]interface{}
	if len(toolParams.Arguments) > 0 {
		_ = json.Unmarshal(toolParams.Arguments, &toolArgs)
	}

	if allowedTools := meta.ParseAllowedTools(); len(allowedTools) > 0 {
		if !allowedTools[toolParams.Name] {
			event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
				EventType: "tool_not_allowed",
				KeyHash:   keyHash,
				ToolName:  toolParams.Name,
				Reason:    "tool_not_in_allowlist",
				Detail:    "Agent identity restricts tool access",
			})
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "tool not allowed for this key",
				"type":  "tool_not_allowed",
			})
			return
		}
	}

	// --- Stateful Session Context ---
	// Resolve sessionID here (before policy evaluation) so we can populate the taint context.
	sessionID := c.GetHeader("X-Loopers-Session-ID")
	ghostSessionID := proxyPkg.ResolveGhostSessionID(sessionID, keyHash)
	var rawProxyKey string
	if pk, exists := c.Get("ProxyKey"); exists {
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

	// Build the OPA session context — populate taint flags and tool history when available.
	sessionCtx := policy.SessionContext{
		ID: sessionID,
	}
	var currentState string
	if sessionID != "" && h.sessionManager != nil {
		if taintFlags, tErr := h.sessionManager.GetTaintFlags(c.Request.Context(), keyHash, sessionID); tErr == nil {
			sessionCtx.TaintFlags = taintFlags
		} else {
			logging.Logger.Warn().Err(tErr).Str("session_id", sessionID).Msg("mcp_failed_to_fetch_taint_flags")
		}

		if toolHistory, hErr := h.sessionManager.GetToolCallHistory(c.Request.Context(), keyHash, sessionID); hErr == nil {
			sessionCtx.ToolsCalled = toolHistory
		} else {
			logging.Logger.Warn().Err(hErr).Str("session_id", sessionID).Msg("mcp_failed_to_fetch_tool_history")
		}

		if traces, trErr := h.sessionManager.GetSessionTraces(c.Request.Context(), keyHash, sessionID); trErr == nil {
			sessionCtx.Traces = traces
		} else {
			logging.Logger.Warn().Err(trErr).Str("session_id", sessionID).Msg("mcp_failed_to_fetch_session_traces")
		}

		// Fetch session FSM state if FSM is configured
		if h.policyEngine != nil {
			if fsm := h.policyEngine.FSM(); fsm != nil {
				if state, sErr := h.sessionManager.GetSessionState(c.Request.Context(), keyHash, sessionID, fsm.InitialState); sErr == nil {
					currentState = state
					sessionCtx.State = state
				} else {
					logging.Logger.Warn().Err(sErr).Str("session_id", sessionID).Msg("mcp_failed_to_fetch_session_state")
				}
			}
		}
	}
	if sessionCtx.TaintFlags == nil {
		sessionCtx.TaintFlags = make(map[string]bool)
	}
	if sessionCtx.ToolsCalled == nil {
		sessionCtx.ToolsCalled = make([]string, 0)
	}
	if sessionCtx.Traces == nil {
		sessionCtx.Traces = make([]policy.SessionTrace, 0)
	}

	br := blastradius.Calculate(toolParams.Name, toolArgs)

	if h.policyEngine != nil {
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

		decision, err := h.policyEngine.Evaluate(c.Request.Context(), policy.EvalInput{
			Agent: policy.AgentContext{
				KeyHash:   keyHash,
				Name:      meta.Name,
				AgentName: meta.AgentName,
				Owner:     meta.Owner,
				Provider:  meta.Provider,
				Tags:      meta.ParseTags(),
			},
			Request: policy.RequestContext{
				Provider:  serverName,
				Method:    "mcp_tool_call",
				ToolName:  toolParams.Name,
				MCPServer: serverName,
				Path:      c.Request.URL.Path,
			},
			// Populated with taint state for cross-call tracking
			Session:   sessionCtx,
			AgentRisk: agentRiskCtx,
			Action: policy.ActionContext{
				Type:               "mcp_tool_call",
				Provider:           serverName,
				ToolName:           toolParams.Name,
				ToolArguments:      toolArgs,
				BlastRadius:        br.Score,
				BlastRadiusTier:    br.Tier,
				BlastRadiusReasons: br.Reasons,
			},
		})
		if err != nil {
			logging.Logger.Error().Err(err).Msg("mcp_policy_evaluation_error")
			event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
				EventType: "policy_evaluation_error",
				KeyHash:   keyHash,
				Provider:  serverName,
				ToolName:  toolParams.Name,
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
			// Proceed

		case "deny":
			if rp != nil && h.budgetClient != nil {
				go func() {
					_, _ = riskprofile.UpdateRiskScore(context.Background(), h.budgetClient.GetUnderlyingClient(), keyHash, 10, false, "policy_block")
				}()
			}
			event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
				EventType: "policy_block",
				KeyHash:   keyHash,
				Provider:  serverName,
				ToolName:  toolParams.Name,
				Reason:    "policy_denied",
				Detail:    decision.Reason,
			})
			logging.Logger.Warn().
				Str("tool_name", toolParams.Name).
				Str("server", serverName).
				Str("session_id", sessionID).
				Int("blast_radius", br.Score).
				Str("blast_radius_tier", br.Tier).
				Str("reason", decision.Reason).
				Msg("mcp_policy_block_agent_friendly")
			abortWithMCPPolicyDenied(c, req, toolParams.Name, decision.Reason)
			return

		case "quarantine":
			if rp != nil && h.budgetClient != nil {
				go func() {
					_, _ = riskprofile.UpdateRiskScore(context.Background(), h.budgetClient.GetUnderlyingClient(), keyHash, 25, true, "quarantine")
				}()
			}
			dur, _ := time.ParseDuration(decision.QuarantineFor)
			if dur <= 0 {
				dur = 1 * time.Hour
			}
			_ = h.budgetClient.GetUnderlyingClient().Set(c.Request.Context(), "loopers:quarantine:"+keyHash, "1", dur).Err()
			event.EmitQuarantineEvent(c.Request.Context(), event.QuarantineEvent{
				KeyHash:       keyHash,
				Reason:        decision.Reason,
				QuarantineFor: decision.QuarantineFor,
				Evidence:      decision.Evidence,
			})
			logging.Logger.Warn().
				Str("tool_name", toolParams.Name).
				Str("server", serverName).
				Str("session_id", sessionID).
				Int("blast_radius", br.Score).
				Str("blast_radius_tier", br.Tier).
				Str("quarantine_for", decision.QuarantineFor).
				Msg("mcp_quarantine_agent")
			abortWithMCPPolicyDenied(c, req, toolParams.Name, "Agent quarantined: "+decision.Reason)
			return

		case "escalate":
			if rp != nil && h.budgetClient != nil {
				go func() {
					_, _ = riskprofile.UpdateRiskScore(context.Background(), h.budgetClient.GetUnderlyingClient(), keyHash, 15, false, "escalate")
				}()
			}
			logging.Logger.Warn().
				Str("tool_name", toolParams.Name).
				Str("server", serverName).
				Str("session_id", sessionID).
				Int("blast_radius", br.Score).
				Str("blast_radius_tier", br.Tier).
				Str("reason", decision.Reason).
				Msg("mcp_escalate_tool_call")
			if h.escalationBroker == nil {
				abortWithMCPPolicyDenied(c, req, toolParams.Name, "Escalation required but broker not configured")
				return
			}
			event.EmitEscalationEvent(c.Request.Context(), event.EscalationEvent{
				KeyHash:    keyHash,
				Provider:   serverName,
				SessionID:  sessionID,
				ToolName:   toolParams.Name,
				Reason:     decision.Reason,
				EscalateTo: decision.EscalateTo,
				Evidence:   decision.Evidence,
			})
			escalationTimeout := 60 * time.Second
			if to := viper.GetDuration("escalation.timeout"); to > 0 {
				escalationTimeout = to
			}
			resp, err := h.escalationBroker.RequestEscalationFromDecision(
				c.Request.Context(), sessionID, meta.AgentName, decision, escalationTimeout,
			)
			if err != nil || resp == nil || !resp.Approved {
				abortWithMCPPolicyDenied(c, req, toolParams.Name, "Escalation denied or timed out: "+decision.Reason)
				return
			}
			// Approved! Proceed.

		case "transform":
			if len(decision.Transforms) > 0 {
				for _, t := range decision.Transforms {
					switch t.Operation {
					case "mask":
						if _, exists := toolArgs[t.Field]; exists {
							toolArgs[t.Field] = "***"
						}
					case "redact":
						delete(toolArgs, t.Field)
					}
				}
				// Re-encode toolArgs back into toolParams.Arguments and req.Params
				if mutatedArgs, err := json.Marshal(toolArgs); err == nil {
					toolParams.Arguments = mutatedArgs
					if mutatedParams, err := json.Marshal(toolParams); err == nil {
						req.Params = mutatedParams
						if mutatedBody, err := json.Marshal(req); err == nil {
							body = mutatedBody
							c.Set("RequestBody", body)
						}
					}
				}
			}

		default:
			abortWithMCPPolicyDenied(c, req, toolParams.Name, "Unknown policy action: "+decision.Action)
			return
		}
	}

	span := trace.SpanFromContext(c.Request.Context())
	span.SetAttributes(attribute.String("gen_ai.system.mcp.tool", toolParams.Name))

	// 1. Circuit Breaker Check
	var cbCount int64
	if sessionID != "" && h.cfg.CircuitBreaker.Enabled {
		if !session.IsValidID(sessionID) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid session ID format"})
			return
		}
		span.SetAttributes(attribute.String("gen_ai.system.session.id", sessionID))
		maxTotalTools := 0
		if maxToolsHeader := c.GetHeader("X-Loopers-Session-Max-Tools"); maxToolsHeader != "" {
			allowOverride := viper.GetBool("mcp.allow_client_max_tools_override")
			if !viper.IsSet("mcp.allow_client_max_tools_override") {
				allowOverride = false // VULN-028: Default to false
			}
			if !allowOverride {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error": "client-controlled max tools are disabled by policy",
					"type":  "invalid_request_error",
				})
				return
			}
			if parsed, err := strconv.Atoi(maxToolsHeader); err == nil && parsed > 0 {
				maxTotalTools = parsed
			}
		}

		cbRes, err := h.circuitBreaker.Check(c.Request.Context(), sessionID, toolParams.Name, toolParams.Arguments, maxTotalTools)
		if err != nil {
			logging.Logger.Warn().Err(err).Msg("MCP circuit breaker check failed")
		} else if cbRes != nil {
			cbCount = cbRes.Count
			if cbRes.TotalTripped {
			event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
				EventType: "circuit_breaker_trip",
				SessionID: sessionID,
				Provider:  serverName,
				ToolName:  toolParams.Name,
				Reason:    "mcp_total_tools_exceeded",
				Detail:    "Total tool invocation limit exceeded",
			})
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "total tool invocation limit exceeded",
				"type":  "mcp_total_tools_exceeded",
			})
			return
		} else if cbRes.Tripped {
			event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
				EventType: "circuit_breaker_trip",
				SessionID: sessionID,
				Provider:  serverName,
				ToolName:  toolParams.Name,
				Reason:    "mcp_circuit_breaker",
				Detail:    "Tool-call circuit breaker tripped",
			})
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "tool-call circuit breaker tripped",
				"type":  "mcp_circuit_breaker",
				"tool":  toolParams.Name,
			})
			return
		}
	}
}

	// 2. Blast Radius Check
	if sessionID != "" && h.sessionManager != nil {
		maxServers := 0
		if maxServersHeader := c.GetHeader("X-Loopers-Session-Max-Servers"); maxServersHeader != "" {
			allowOverride := viper.GetBool("mcp.allow_client_max_servers_override")
			if !viper.IsSet("mcp.allow_client_max_servers_override") {
				allowOverride = false // VULN-029: Default to false
			}
			if !allowOverride {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error": "client-controlled blast radius is disabled by policy",
					"type":  "invalid_request_error",
				})
				return
			}
			if parsed, err := strconv.Atoi(maxServersHeader); err == nil && parsed > 0 {
				maxServers = parsed
			}
		}
		if maxServers > 0 {
			allowed, err := h.sessionManager.CheckBlastRadius(c.Request.Context(), keyHash, sessionID, serverName, maxServers)
			if err != nil {
				logging.Logger.Error().Err(err).Msg("MCP blast radius check failed")
			} else if !allowed {
				event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
					EventType: "blast_radius_block",
					SessionID: sessionID,
					Provider:  serverName, // in MCP context we use provider for serverName
					Reason:    "blast_radius_exceeded",
					Detail:    "Maximum number of distinct servers exceeded",
				})
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "maximum number of distinct servers exceeded",
					"type":  "mcp_blast_radius_exceeded",
				})
				return
			}
		}
	}

	// 2.5 Read-Only Tool Memoization
	memoizeEnabled := h.cfg.MemoizeReadOnly || viper.GetBool("mcp.memoize_read_only")
	var memoKey string
	if memoizeEnabled && ghostSessionID != "" && br.Score <= 15 && h.budgetClient != nil {
		callHash := fmt.Sprintf("%s|%s", toolParams.Name, hashArgs(toolParams.Arguments))
		memoKey = fmt.Sprintf("loopers:mcp:memo:{%s}:%s:%s", keyHash, ghostSessionID, callHash)

		cachedBytes, err := h.budgetClient.GetUnderlyingClient().Get(c.Request.Context(), memoKey).Bytes()
		if err == nil && len(cachedBytes) > 0 {
			var rpcResp map[string]interface{}
			if jsonErr := json.Unmarshal(cachedBytes, &rpcResp); jsonErr == nil {
				rpcResp["id"] = req.ID

				// If repeated 3+ times in the window, append self-correcting hint
				if cbCount >= 3 {
					if resMap, ok := rpcResp["result"].(map[string]interface{}); ok {
						if contentList, ok := resMap["content"].([]interface{}); ok && len(contentList) > 0 {
							if firstItem, ok := contentList[0].(map[string]interface{}); ok {
								if textVal, ok := firstItem["text"].(string); ok {
									firstItem["text"] = textVal + "\n[Loopers Notice: Identical read-only tool call repeated in this session; returning cached output. Proceed with synthesis instead of re-calling.]"
								}
							}
						}
					}
				}

				if updatedResp, marshalErr := json.Marshal(rpcResp); marshalErr == nil {
					c.Header("X-Loopers-MCP-Memoized", "true")
					savedCost := h.pricingStore.GetToolCost(toolParams.Name)
					if savedCost > 0 {
						if !viper.GetBool("server.strip_budget_headers") {
							c.Header("X-Loopers-Saved-USD", fmt.Sprintf("%.6f", savedCost))
						}
						if h.budgetClient != nil {
							_ = h.budgetClient.RecordSavings(context.Background(), keyHash, savedCost)
						}
					}
					c.Data(http.StatusOK, "application/json", updatedResp)
					return
				}
			}
		}
	}

	// 3. Budget Check
	toolCost := h.pricingStore.GetToolCost(toolParams.Name)
	if toolCost > 0 {
		err = h.budgetClient.LeaseManager.Acquire(c.Request.Context(), keyHash, toolCost, 1.0)
		if err != nil {
			if errors.Is(err, budget.ErrBudgetExceeded) {
				event.EmitBlockEvent(c.Request.Context(), event.BlockEvent{
					EventType: "budget_block",
					KeyHash:   keyHash,
					Provider:  serverName,
					ToolName:  toolParams.Name,
					Reason:    "budget_exceeded",
					Detail:    "MCP tool invocation budget exceeded",
				})
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": "budget exceeded",
					"type":  "mcp_budget_exceeded",
				})
				return
			}
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "Service Unavailable"})
			return
		}
	}

	// 4. Post-allow: Record tool call history and auto-taint flagging
	if sessionID != "" && h.sessionManager != nil && session.IsValidID(sessionID) {
		// Record the tool call in history (async — non-blocking, best-effort)
		go func() {
			if appendErr := h.sessionManager.AppendToolCall(context.Background(), keyHash, sessionID, toolParams.Name); appendErr != nil {
				logging.Logger.Warn().Err(appendErr).Str("tool_name", toolParams.Name).Msg("mcp_failed_to_append_tool_history")
			}
		}()

		// Record the tool call in the transient session buffer
		go func() {
			_ = h.sessionManager.AppendSessionTrace(context.Background(), keyHash, sessionID, policy.SessionTrace{
				Timestamp: time.Now().Unix(),
				Type:      "mcp_tool_call",
				Provider:  serverName,
				ToolName:  toolParams.Name,
				Arguments: toolArgs,
			})
		}()

		// Auto-taint: if this tool accesses sensitive resources, flag the session.
		if isSensitiveTaintTool(toolParams.Name) {
			go func() {
				if taintErr := h.sessionManager.AppendTaintFlag(context.Background(), keyHash, sessionID, "secret_accessed"); taintErr != nil {
					logging.Logger.Warn().Err(taintErr).Str("tool_name", toolParams.Name).Msg("mcp_failed_to_append_taint_flag")
				}
				logging.Logger.Info().
					Str("session_id", sessionID).
					Str("tool_name", toolParams.Name).
					Str("taint_flag", "secret_accessed").
					Msg("mcp_taint_flag_set")
			}()
			if h.riskProfileCfg.Enabled && h.budgetClient != nil {
				go func() {
					_, _ = riskprofile.AddPersistentTaintFlag(context.Background(), h.budgetClient.GetUnderlyingClient(), keyHash, "secret_accessed")
				}()
			}
		}

		// Handle FSM transitions if configured
		if h.policyEngine != nil {
			if fsm := h.policyEngine.FSM(); fsm != nil {
				go func() {
					for _, trans := range fsm.Transitions {
						if trans.From == currentState {
							if trans.Trigger == toolParams.Name || trans.Trigger == "mcp_tool_call" {
								_ = h.sessionManager.SetSessionState(context.Background(), keyHash, sessionID, trans.To)
								break
							}
						}
					}
				}()
			}
		}
	}

	// Forward the request and restore body
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	ctx := c.Request.Context()
	tracer := otel.Tracer("loopers-proxy")
	ctx, toolSpan := tracer.Start(ctx, "loopers.mcp.tool_call", trace.WithSpanKind(trace.SpanKindClient))
	toolSpan.SetAttributes(
		attribute.String("mcp.tool.name", toolParams.Name),
		attribute.String("mcp.server.name", serverName),
		attribute.Float64("mcp.tool.cost", toolCost),
		attribute.String("loopers.enforcement.action", "allowed"),
	)
	defer toolSpan.End()

	ctx = context.WithValue(ctx, mcpSessionIDCtxKey, sessionID)
	ctx = context.WithValue(ctx, mcpGhostSessionIDCtxKey, ghostSessionID)
	ctx = context.WithValue(ctx, mcpProxyKeyCtxKey, rawProxyKey)
	ctx = context.WithValue(ctx, proxyPkg.RawProxyKeyCtx, rawProxyKey)
	ctx = context.WithValue(ctx, mcpServerNameCtxKey, serverName)
	ctx = context.WithValue(ctx, mcpToolNameCtxKey, toolParams.Name)
	ctx = context.WithValue(ctx, mcpMethodCtxKey, "tools/call")
	ctx = context.WithValue(ctx, mcpServerCtxKey, serverName)
	if memoKey != "" {
		ctx = context.WithValue(ctx, mcpMemoKeyCtxKey, memoKey)
	}
	c.Request = c.Request.WithContext(ctx)

	h.signRequest(c, body)
	h.forward(c, targetURL, &keyHash, toolCost)
}

func (h *Handler) signRequest(c *gin.Context, body []byte) {
	if h.signer != nil && h.signer.Enabled {
		sig, t, err := h.signer.Sign(body)
		if err == nil {
			sigHeader := h.signer.FormatHeader(t, sig)
			c.Request.Header.Set("X-Loopers-Signature", sigHeader)
			ctx := context.WithValue(c.Request.Context(), proxyPkg.RequestSignatureCtx, sigHeader)
			c.Request = c.Request.WithContext(ctx)
		}
	}
}

func (h *Handler) forward(c *gin.Context, targetURL string, keyHash *string, cost float64) {
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, MCPTargetCtx, targetURL)
	if keyHash != nil {
		ctx = context.WithValue(ctx, proxyPkg.ProxyKeyHashCtx, *keyHash)
		ctx = context.WithValue(ctx, proxyPkg.RequestCostCtx, cost)
	}
	if rawProxyKey, ok := ctx.Value(mcpProxyKeyCtxKey).(string); ok && rawProxyKey != "" {
		ctx = context.WithValue(ctx, proxyPkg.RawProxyKeyCtx, rawProxyKey)
	}
	c.Request = c.Request.WithContext(ctx)

	// Strip the /mcp/:server prefix before forwarding if needed
	// Example: /mcp/filesystem/tools/call -> /tools/call
	path := c.Request.URL.Path
	prefix := fmt.Sprintf("/mcp/%s", c.Param("server"))
	c.Request.URL.Path = strings.TrimPrefix(path, prefix)
	if c.Request.URL.RawPath != "" {
		c.Request.URL.RawPath = strings.TrimPrefix(c.Request.URL.RawPath, prefix)
	}

	sessionID, _ := ctx.Value(mcpSessionIDCtxKey).(string)
	serverName, _ := ctx.Value(mcpServerNameCtxKey).(string)
	toolName, _ := ctx.Value(mcpToolNameCtxKey).(string)

	var w *responseBodyWriter
	if sessionID != "" && h.sessionManager != nil && keyHash != nil {
		w = &responseBodyWriter{body: &bytes.Buffer{}, ResponseWriter: c.Writer}
		c.Writer = w
	}

	h.proxy.ServeHTTP(c.Writer, c.Request)

	if w != nil && keyHash != nil {
		respBody := w.body.Bytes()
		go func() {
			completion := parseMCPResponse(respBody)
			if len(completion) > 512 {
				completion = completion[:512]
			}
			_ = h.sessionManager.AppendSessionTrace(context.Background(), *keyHash, sessionID, policy.SessionTrace{
				Timestamp: time.Now().Unix(),
				Type:      "mcp_tool_response",
				Provider:  serverName,
				ToolName:  toolName,
				Content:   completion,
			})
		}()
	}
}

// jsonRPCIDFromBody extracts the "id" field from a raw JSON-RPC body for error responses.
// Returns nil if parsing fails.
func jsonRPCIDFromBody(body []byte) any {
	var envelope struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.ID == nil {
		return nil
	}
	// Try int, then string
	var idInt int64
	if err := json.Unmarshal(envelope.ID, &idInt); err == nil {
		return idInt
	}
	var idStr string
	if err := json.Unmarshal(envelope.ID, &idStr); err == nil {
		return idStr
	}
	return nil
}

type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func parseMCPResponse(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}

	// Case 1: Success result.content
	if result, ok := data["result"].(map[string]interface{}); ok {
		if content, ok := result["content"].([]interface{}); ok {
			var parts []string
			for _, part := range content {
				if partMap, ok := part.(map[string]interface{}); ok {
					if text, ok := partMap["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
			return strings.Join(parts, "\n")
		}
	}

	// Case 2: Error response
	if errObj, ok := data["error"].(map[string]interface{}); ok {
		if msg, ok := errObj["message"].(string); ok {
			return "Error: " + msg
		}
	}

	return ""
}
