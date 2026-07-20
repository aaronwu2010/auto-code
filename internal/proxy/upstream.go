package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
)

type UpstreamProxy struct {
	mu       sync.RWMutex
	target   *url.URL
	proxy    *httputil.ReverseProxy
	enabled  bool
}

func NewUpstreamProxy(targetURL string) (*UpstreamProxy, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("parsing target URL: %w", err)
	}

	return &UpstreamProxy{
		target:  target,
		proxy:   httputil.NewSingleHostReverseProxy(target),
		enabled: true,
	}, nil
}

func (p *UpstreamProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	enabled := p.enabled
	p.mu.RUnlock()

	if !enabled {
		http.Error(w, "proxy disabled", http.StatusServiceUnavailable)
		return
	}

	p.proxy.ServeHTTP(w, r)
}

func (p *UpstreamProxy) Relay(ctx context.Context, req *http.Request) (*http.Response, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.enabled {
		return nil, fmt.Errorf("proxy disabled")
	}

	return http.DefaultClient.Do(req.WithContext(ctx))
}

func (p *UpstreamProxy) Enable() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = true
}

func (p *UpstreamProxy) Disable() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = false
}

func (p *UpstreamProxy) IsEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.enabled
}