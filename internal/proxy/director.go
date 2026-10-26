package proxy

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/spf13/viper"
	"github.com/try-loopers/loopers-oss/internal/logging"
	"github.com/try-loopers/loopers-oss/internal/netutil"
	"github.com/try-loopers/loopers-oss/internal/provider"
)

// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

// Context keys used inside loopers proxy
const (
	ProviderKeyCtx      ContextKey = "ProviderKey"
	ProviderCtx         ContextKey = "Provider" // "openai" or "anthropic"
	ProviderInstanceCtx ContextKey = "ProviderInstance"
	ProxyKeyHashCtx     ContextKey = "ProxyKeyHash"
	RawProxyKeyCtx      ContextKey = "RawProxyKey"
	RequestCostCtx      ContextKey = "RequestCost"
	ModelCtx            ContextKey = "Model"
	RequestSignatureCtx ContextKey = "RequestSignature"
)

// Proxy handles forwarding requests to upstream providers.
type Proxy struct {
	reverseProxy *httputil.ReverseProxy
}

// NewProxy creates a new ReverseProxy wrapper with highly-optimized production settings.
func NewProxy(modifyResponse func(*http.Response) error) *Proxy {
	timeout := viper.GetInt("server.upstream_timeout_seconds")
	if timeout <= 0 {
		timeout = 30
	}
	transport := &http.Transport{
		DialContext:           netutil.SecureDialContext,
		MaxIdleConns:          4000,
		MaxIdleConnsPerHost:   4000,
		ForceAttemptHTTP2:     true,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: time.Duration(timeout) * time.Second,
	}

	director := func(req *http.Request) {
		ctx := req.Context()
		providerKey, _ := ctx.Value(ProviderKeyCtx).(string)
		prov, _ := ctx.Value(ProviderInstanceCtx).(provider.Provider)

		if prov != nil {
			target, _ := url.Parse(prov.BaseURL())
			if target != nil {
				req.URL.Scheme = target.Scheme
				req.URL.Host = target.Host
				req.Host = target.Host
			}
			req.URL.Path = prov.RewritePath(req.URL.Path)
			prov.InjectAuth(req, providerKey)
		}
	}

	errorHandler := func(w http.ResponseWriter, r *http.Request, err error) {
		if err == context.Canceled || r.Context().Err() == context.Canceled {
			logging.Logger.Debug().Msg("Client cancelled connection; aborting backend request")
			return
		}
		logging.Logger.Error().Err(err).Msg("Reverse proxy forwarding error")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"Bad gateway forwarding request","type":"bad_gateway_error"}}`))
	}

	rp := &httputil.ReverseProxy{
		Director:       director,
		Transport:      transport,
		FlushInterval:  0, // Handle flushes explicitly in the streaming interceptor
		ModifyResponse: modifyResponse,
		ErrorHandler:   errorHandler,
	}

	return &Proxy{reverseProxy: rp}
}

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.reverseProxy.ServeHTTP(w, r)
}
