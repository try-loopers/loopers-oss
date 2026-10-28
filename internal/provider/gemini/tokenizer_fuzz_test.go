package gemini

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// FuzzCountGeminiTokensFallback tests that token counting (both via API endpoint and local fallback)
// doesn't panic or block when provided with arbitrary byte slices (e.g. malformed JSON).
func FuzzCountGeminiTokensFallback(f *testing.F) {
	// Warm up fallback encoding once before fuzz workers start to avoid any initialization contention
	_, _ = getCL100kEncoding()

	// Add some seed corpus examples
	f.Add([]byte(`{"contents":[{"role":"user","parts":[{"text":"hello world"}]}]}`))
	f.Add([]byte(`{"contents":[]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`not json`))
	f.Add([]byte(``))

	// Fast in-memory client that mimics the Gemini countTokens endpoint without TCP socket exhaustion
	mockClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"totalTokens":1}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	p := NewGeminiProviderWithOptions("http://mock-gemini.local", mockClient)

	f.Fuzz(func(t *testing.T, data []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Exercise both provider token count (mocked in-memory) and fallback
		_, _ = p.CountInputTokens(ctx, "gemini-3.5-flash", data, "test-key")
		_, _ = countGeminiTokensFallback(data)
	})
}
