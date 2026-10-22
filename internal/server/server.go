package server

import (
	"context"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"
	"github.com/try-loopers/loopers-oss/internal/a2a"
	"github.com/try-loopers/loopers-oss/internal/alerting"
	"github.com/try-loopers/loopers-oss/internal/budget"
	"github.com/try-loopers/loopers-oss/internal/inspector"
	"github.com/try-loopers/loopers-oss/internal/keyring"
	"github.com/try-loopers/loopers-oss/internal/logging"
	"github.com/try-loopers/loopers-oss/internal/loop"
	"github.com/try-loopers/loopers-oss/internal/mcp"
	"github.com/try-loopers/loopers-oss/internal/netutil"
	"github.com/try-loopers/loopers-oss/internal/otel"
	"github.com/try-loopers/loopers-oss/internal/policy"
	"github.com/try-loopers/loopers-oss/internal/pricing"
	"github.com/try-loopers/loopers-oss/internal/provider"
	"github.com/try-loopers/loopers-oss/internal/provider/anthropic"
	"github.com/try-loopers/loopers-oss/internal/provider/azure"
	"github.com/try-loopers/loopers-oss/internal/provider/bedrock"
	"github.com/try-loopers/loopers-oss/internal/provider/cohere"
	"github.com/try-loopers/loopers-oss/internal/provider/deepseek"
	"github.com/try-loopers/loopers-oss/internal/provider/fireworks"
	"github.com/try-loopers/loopers-oss/internal/provider/gemini"
	"github.com/try-loopers/loopers-oss/internal/provider/generic"
	"github.com/try-loopers/loopers-oss/internal/provider/groq"
	"github.com/try-loopers/loopers-oss/internal/provider/mistral"
	"github.com/try-loopers/loopers-oss/internal/provider/ollama"
	"github.com/try-loopers/loopers-oss/internal/provider/openai"
	"github.com/try-loopers/loopers-oss/internal/provider/openrouter"
	"github.com/try-loopers/loopers-oss/internal/provider/together"
	"github.com/try-loopers/loopers-oss/internal/provider/vllm"
	"github.com/try-loopers/loopers-oss/internal/provider/xai"
	"github.com/try-loopers/loopers-oss/internal/proxy"
	"github.com/try-loopers/loopers-oss/internal/ratelimit"
	"github.com/try-loopers/loopers-oss/internal/riskprofile"
	"github.com/try-loopers/loopers-oss/internal/session"
	"github.com/try-loopers/loopers-oss/internal/signature"
	"go.opentelemetry.io/otel/trace"
)

type serverContextKey string

const (
	isStreamCtx        serverContextKey = "IsStream"
	inputPriceCtx      serverContextKey = "InputPrice"
	outputPriceCtx     serverContextKey = "OutputPrice"
	cacheReadPriceCtx  serverContextKey = "CacheReadPrice"
	cacheWritePriceCtx serverContextKey = "CacheWritePrice"
	inputTokensCtx     serverContextKey = "InputTokens"
	sessionIDCtx       serverContextKey = "SessionID"
	sessionBudgetCtx   serverContextKey = "SessionBudget"
	sessionMaxStepsCtx serverContextKey = "SessionMaxSteps"
	startTimeCtx       serverContextKey = "StartTime"
	keyNameCtx         serverContextKey = "KeyName"
	requestIDCtx       serverContextKey = "ProxyRequestID"
	agentNameCtx       serverContextKey = "AgentName"
)

// Server coordinates the proxy operations and HTTP routing.
type Server struct {
	router           *gin.Engine
	redis            *budget.Client
	pricing          *pricing.Store
	proxy            *proxy.Proxy
	registry         *provider.Registry
	alerter          *alerting.Alerter
	loopDetector     *loop.Detector
	shadowMode       bool
	proxyGroup       *gin.RouterGroup // exposed so tests can register routes with BodyBuffer applied
	otelEnabled      bool
	otelShutdown     func(context.Context) error
	mcpHandler       *mcp.Handler
	riskProfileCfg   riskprofile.Config
	dlpCfg           inspector.DLPConfig
	rateLimiter      *ratelimit.Limiter
	sessionManager   *session.Manager
	policyEngine     *policy.Engine
	signer           *signature.Signer
	escalationBroker *a2a.EscalationBroker
	jwksValidator    *keyring.JWKSValidator
	ctx              context.Context
	cancel           context.CancelFunc
}

// NewServer initializes and builds the HTTP server with middlewares and ReverseProxy configuration.
func NewServer(redisClient *budget.Client, pricingStore *pricing.Store) *Server {
	if viper.GetBool("testing.allow_private_urls") && viper.GetString("env") != "development" && !strings.HasSuffix(os.Args[0], ".test") && !strings.HasSuffix(os.Args[0], ".test.exe") {
		logging.Logger.Fatal().Msg("CRITICAL: testing.allow_private_urls is enabled but env is not 'development'. This disables SSRF protection and is forbidden in production.")
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	reg := provider.NewRegistry()
	mustRegister := func(p provider.Provider) {
		if err := reg.Register(p); err != nil {
			logging.Logger.Fatal().Err(err).Msg("Failed to register provider")
		}
	}
	mustRegister(openai.NewOpenAIProvider())
	mustRegister(anthropic.NewAnthropicProvider())
	mustRegister(gemini.NewGeminiProvider())
	mustRegister(bedrock.NewBedrockProvider())
	mustRegister(azure.NewAzureProvider())
	mustRegister(mistral.NewMistralProvider())
	mustRegister(groq.NewGroqProvider())
	mustRegister(cohere.NewCohereProvider())
	mustRegister(deepseek.NewDeepSeekProvider())
	mustRegister(together.NewTogetherProvider())
	mustRegister(ollama.NewOllamaProvider())
	mustRegister(fireworks.NewFireworksProvider())
	mustRegister(xai.NewXAIProvider())
	mustRegister(vllm.NewVLLMProvider())
	mustRegister(openrouter.NewOpenRouterProvider())

	type GenericProviderConfig struct {
		Name    string `mapstructure:"name"`
		BaseURL string `mapstructure:"base_url"`
	}
	var genericProviders []GenericProviderConfig
	if err := viper.UnmarshalKey("generic_providers", &genericProviders); err == nil {
		// Pre-compile name validation: alphanumeric, dash, underscore only.
		// This prevents malformed Gin route patterns (G1).
		validName := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
		for _, gp := range genericProviders {
			if gp.Name == "" || gp.BaseURL == "" {
				logging.Logger.Warn().Msgf("Skipping generic provider with empty name or base_url")
				continue
			}
			// G1: Reject names with special characters
			if !validName.MatchString(gp.Name) {
				logging.Logger.Error().Msgf("Invalid generic provider name %q: only alphanumeric, dash, and underscore are allowed", gp.Name)
				continue
			}

			// SSRF protection
			if netutil.IsPrivateURL(gp.BaseURL) {
				logging.Logger.Error().Msgf("SSRF protection: rejecting generic provider %q because base URL %q resolves to a private or unresolvable IP", gp.Name, gp.BaseURL)
				continue
			}

			// S1 & S3: Detect collision with built-in or previously registered provider
			if _, err := reg.Get(gp.Name); err == nil {
				logging.Logger.Error().Msgf("Generic provider %q conflicts with an already-registered provider; skipping to prevent shadowing", gp.Name)
				continue
			}
			if err := reg.Register(generic.NewGenericProvider(gp.Name, gp.BaseURL)); err != nil {
				logging.Logger.Error().Err(err).Msgf("Failed to register generic provider %q", gp.Name)
				continue
			}
			logging.Logger.Info().Msgf("Registered generic provider: %s (%s)", gp.Name, gp.BaseURL)
		}
	}

	var alertingCfg alerting.AlertingConfig
	var alerter *alerting.Alerter
	var loopDetector *loop.Detector
	var rateLimiter *ratelimit.Limiter
	var sessionManager *session.Manager
	if redisClient != nil {
		if err := viper.UnmarshalKey("alerting", &alertingCfg); err == nil {
			alerter = alerting.NewAlerter(alertingCfg, redisClient.GetUnderlyingClient())
		}

		var loopCfg loop.Config
		if err := viper.UnmarshalKey("loop_detection", &loopCfg); err == nil && loopCfg.Enabled {
			loopDetector = loop.NewDetector(loopCfg, redisClient.GetUnderlyingClient())
		}

		var rlCfg ratelimit.Config
		if err := viper.UnmarshalKey("rate_limit", &rlCfg); err == nil && rlCfg.Enabled {
			rateLimiter = ratelimit.NewLimiter(rlCfg, redisClient.GetUnderlyingClient())
		}

		sessionManager = session.NewManager(redisClient.GetUnderlyingClient())
	}

	var policyEngine *policy.Engine
	var policyCfg policy.Config

	ctx, cancel := context.WithCancel(context.Background())

	if err := viper.UnmarshalKey("policy", &policyCfg); err == nil && policyCfg.Enabled {
		engine, err := policy.NewEngine(policyCfg)
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("Failed to initialize policy engine")
		}
		policyEngine = engine

		// Start watcher in background
		go func() {
			if err := policyEngine.StartWatcher(ctx); err != nil {
				logging.Logger.Error().Err(err).Msg("Failed to start policy watcher")
			}
		}()
	}

	var sigSigner *signature.Signer
	if viper.GetBool("policy.signature.enabled") {
		sigCfg := signature.Config{
			Enabled: true,
			Type:    viper.GetString("policy.signature.type"),
			Secret:  viper.GetString("policy.signature.secret"),
		}
		signer, err := signature.NewSigner(sigCfg)
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("Failed to initialize cryptographic signer")
		}
		sigSigner = signer
	}

	var riskProfileCfg riskprofile.Config = riskprofile.DefaultConfig()
	if redisClient != nil {
		_ = viper.UnmarshalKey("risk_profile", &riskProfileCfg)
	}

	var dlpCfg inspector.DLPConfig
	_ = viper.UnmarshalKey("server.dlp", &dlpCfg)

	var mcpCfg mcp.Config
	var mcpHandler *mcp.Handler
	if err := viper.UnmarshalKey("mcp", &mcpCfg); err == nil && mcpCfg.Enabled {
		if redisClient != nil {
			mcpHandler = mcp.NewHandler(mcpCfg, riskProfileCfg, redisClient, pricingStore, alerter, sessionManager, policyEngine, sigSigner)
		} else {
			logging.Logger.Error().Msg("MCP is enabled but Redis client is not initialized. MCP routing will be disabled.")
		}
	}

	shadowMode := viper.GetBool("server.shadow_mode")

	var otelCfg otel.Config
	var otelShutdown func(context.Context) error
	var otelEnabled bool
	if err := viper.UnmarshalKey("otel", &otelCfg); err == nil {
		otelEnabled = otelCfg.Enabled
		if shutdown, err := otel.Init(otelCfg, alerting.Version); err != nil {
			logging.Logger.Error().Err(err).Msg("failed to initialize OTel")
			otelEnabled = false
		} else {
			otelShutdown = shutdown
		}
	}

	var jwksValidator *keyring.JWKSValidator
	var escalationBroker *a2a.EscalationBroker
	if viper.GetBool("zsp.enabled") {
		var err error
		jwksValidator, err = keyring.NewJWKSValidator(ctx, viper.GetString("zsp.jwks_url"))
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("Failed to initialize JWKS validator")
		}
		if redisClient != nil {
			escalationBroker = a2a.NewEscalationBroker(redisClient.GetUnderlyingClient(), viper.GetString("zsp.escalation_secret"))
			if mcpHandler != nil {
				mcpHandler.SetEscalationBroker(escalationBroker)
			}
		}
	}

	s := &Server{
		router:           r,
		redis:            redisClient,
		pricing:          pricingStore,
		registry:         reg,
		alerter:          alerter,
		loopDetector:     loopDetector,
		shadowMode:       shadowMode,
		otelEnabled:      otelEnabled,
		otelShutdown:     otelShutdown,
		mcpHandler:       mcpHandler,
		riskProfileCfg:   riskProfileCfg,
		dlpCfg:           dlpCfg,
		rateLimiter:      rateLimiter,
		sessionManager:   sessionManager,
		policyEngine:     policyEngine,
		signer:           sigSigner,
		escalationBroker: escalationBroker,
		jwksValidator:    jwksValidator,
		ctx:              ctx,
		cancel:           cancel,
	}

	// Setup custom ReverseProxy with modifyResponse callback
	s.proxy = proxy.NewProxy(s.modifyResponse)

	s.setupRoutes()
	return s
}

// GetOtelShutdown returns the shutdown function for OpenTelemetry.
func (s *Server) GetOtelShutdown() func(context.Context) error {
	return s.otelShutdown
}

// Shutdown gracefully stops background tasks associated with the Server.
func (s *Server) Shutdown() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.alerter != nil {
		s.alerter.Close()
	}
}

func (s *Server) setupRoutes() {
	s.router.Use(RequestID())
	s.router.Use(Recovery())
	s.router.Use(RequestLogger())

	maxPayloadBytes := int64(viper.GetInt("server.max_payload_bytes"))
	if maxPayloadBytes <= 0 {
		maxPayloadBytes = 2 << 20 // 2MB default
	}
	s.router.Use(MaxBytesReader(maxPayloadBytes)) // Wrap request body with configurable limit
	s.router.Use(KeyExtractor(s.jwksValidator))

	s.router.GET("/health", s.handleHealth)
	s.router.GET("/budget/status", s.ipRateLimiter(60, time.Minute), s.handleBudgetStatus)

	s.proxyGroup = s.router.Group("/")

	maxInflight := viper.GetInt("server.max_inflight")
	if maxInflight <= 0 {
		maxInflight = 2000
	}
	s.proxyGroup.Use(ConcurrencyLimiter(maxInflight))
	s.proxyGroup.Use(otel.TraceMiddleware(s.otelEnabled))
	s.proxyGroup.Use(BodyBuffer())

	for _, p := range s.registry.All() {
		providerName := p.Name()
		s.proxyGroup.POST("/"+providerName+"/*path", func(c *gin.Context) {
			s.handleProxy(c, providerName)
		})
	}

	if s.mcpHandler != nil {
		mcpGroup := s.router.Group("/mcp")
		mcpGroup.Use(ConcurrencyLimiter(maxInflight))
		mcpGroup.Use(otel.TraceMiddleware(s.otelEnabled))
		mcpGroup.Use(BodyBuffer())
		mcpGroup.Any("/:server/*path", func(c *gin.Context) {
			s.mcpHandler.HandleMCP(c)
		})
	}
}

// RegisterProviderRoute registers a provider and adds its route to the proxy group (with BodyBuffer applied).
// This is intended for use in tests only.
func (s *Server) RegisterProviderRoute(p provider.Provider) {
	_ = s.registry.Register(p)
	providerName := p.Name()
	s.proxyGroup.POST("/"+providerName+"/*path", func(c *gin.Context) {
		s.handleProxy(c, providerName)
	})
}

func (s *Server) handleHealth(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	if err := s.redis.Ping(ctx); err != nil {
		logging.Logger.Error().Err(err).Msg("Health check failed: Redis unreachable")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  "Redis connection down",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

func (s *Server) handleBudgetStatus(c *gin.Context) {
	var meta *keyring.KeyMetadata
	var keyHash string

	if claims, exists := c.Get(JWTClaimsCtx); exists {
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
			_, err := keyring.ValidateDPoPAndReplay(c.Request.Context(), s.redis.GetUnderlyingClient(), dpopHeader, c.Request.Method, requestURL, meta.Jkt)
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
		meta, err = keyring.GetKeyMetadata(c.Request.Context(), s.redis.GetUnderlyingClient(), keyHash)
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

	status, err := s.redis.GetBudgetStatus(c.Request.Context(), keyHash)
	if err != nil {
		logging.Logger.Error().Err(err).Msg("Failed to query budget status")
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to get budget status from cache"})
		return
	}

	c.JSON(http.StatusOK, status)
}

func (s *Server) GetRouter() *gin.Engine {
	return s.router
}

// PathAuthWrapper intercepts requests with proxy keys in the URL path,
// extracts them, and modifies the request to use standard HTTP headers before routing.
// This enables "Zero-SDK" integration for pre-built agents that don't support custom headers.
func PathAuthWrapper(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathParts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 3)
		// Check if the first path segment is a Loopers key (lp-...) or a JWT (eyJ...)
		if len(pathParts) >= 2 && (strings.HasPrefix(pathParts[0], "lp-") || strings.HasPrefix(pathParts[0], "eyJ")) {
			proxyKey := pathParts[0]

			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				// Move real provider key to X-Loopers-Provider-Key
				r.Header.Set("X-Loopers-Provider-Key", strings.TrimPrefix(authHeader, "Bearer "))
			}

			// Set the extracted Loopers key as the new Authorization header
			r.Header.Set("Authorization", "Bearer "+proxyKey)

			// Rewrite URL path to strip the proxy key
			r.URL.Path = "/" + strings.Join(pathParts[1:], "/")
		}
		next.ServeHTTP(w, r)
	})
}

// GetHandler returns the main HTTP handler wrapped with necessary pre-routing middlewares.
func (s *Server) GetHandler() http.Handler {
	return PathAuthWrapper(s.router)
}

// GetAdminRouter retrieves the Gin engine for the admin port.
func (s *Server) GetAdminRouter() *gin.Engine {
	adminRouter := gin.New()
	adminRouter.Use(gin.Recovery())
	adminRouter.GET("/health", s.handleHealth)
	adminRouter.GET("/metrics", gin.WrapH(promhttp.Handler()))
	return adminRouter
}

// GetRegistry retrieves the provider registry. Primarily used for testing.
func (s *Server) GetRegistry() *provider.Registry {
	return s.registry
}

// detachedTraceContext creates a new background context but carries over the trace span context.
// This is used for firing alerts in goroutines without risking cancellation of the original context.
func detachedTraceContext(ctx context.Context) context.Context {
	return trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
}

// ipRateLimiter provides a simple fixed-window IP-based rate limit to prevent abuse of endpoints.
func (s *Server) ipRateLimiter(limit int64, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if ip == "" {
			c.Next()
			return
		}

		key := "loopers:rl:ip:" + ip
		ctx := c.Request.Context()
		rdb := s.redis.GetUnderlyingClient()

		count, err := redis.NewScript(`
			local c = redis.call('INCR', KEYS[1])
			if c == 1 then
				redis.call('EXPIRE', KEYS[1], tonumber(ARGV[1]))
			end
			return c
		`).Run(ctx, rdb, []string{key}, int(window.Seconds())).Int64()

		if err == nil && count > limit {
			c.Header("X-RateLimit-Remaining", "0")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded for this endpoint",
				"type":  "rate_limit_exceeded",
			})
			return
		}
		c.Next()
	}
}
