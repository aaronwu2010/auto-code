package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
)

type AuthCodeListener struct {
	mu             sync.Mutex
	server         *http.Server
	port           int
	codeCh         chan string
	errCh          chan error
	expectedState  string
	pendingResp    http.ResponseWriter
}

func NewAuthCodeListener() *AuthCodeListener {
	return &AuthCodeListener{
		codeCh: make(chan string, 1),
		errCh:  make(chan error, 1),
	}
}

func (l *AuthCodeListener) Start(ctx context.Context) (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("listen: %w", err)
	}

	l.port = listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", l.handleCallback)

	l.server = &http.Server{
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		if err := l.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			l.errCh <- err
		}
	}()

	return l.port, nil
}

func (l *AuthCodeListener) GetPort() int {
	return l.port
}

func (l *AuthCodeListener) WaitForAuthorizationCode(ctx context.Context, state string) (string, error) {
	l.mu.Lock()
	l.expectedState = state
	l.mu.Unlock()

	select {
	case code := <-l.codeCh:
		return code, nil
	case err := <-l.errCh:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (l *AuthCodeListener) HandleSuccessRedirect(w http.ResponseWriter, scopes []string) {
	l.mu.Lock()
	if l.pendingResp != nil {
		if shouldUseClaudeAIAuth(scopes) {
			http.Redirect(l.pendingResp, nil, "https://claude.ai/auth/success", http.StatusFound)
		} else {
			http.Redirect(l.pendingResp, nil, "https://console.anthropic.com/auth/success", http.StatusFound)
		}
		l.pendingResp = nil
	}
	l.mu.Unlock()
}

func (l *AuthCodeListener) HandleErrorRedirect(w http.ResponseWriter) {
	l.mu.Lock()
	if l.pendingResp != nil {
		http.Error(l.pendingResp, "Authentication failed. Please try again.", http.StatusBadRequest)
		l.pendingResp = nil
	}
	l.mu.Unlock()
}

func (l *AuthCodeListener) Close() error {
	if l.server != nil {
		return l.server.Close()
	}
	return nil
}

func (l *AuthCodeListener) handleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	if errParam := query.Get("error"); errParam != "" {
		l.errCh <- fmt.Errorf("OAuth error: %s - %s", errParam, query.Get("error_description"))
		l.HandleErrorRedirect(w)
		return
	}

	code := query.Get("code")
	state := query.Get("state")

	if code == "" {
		l.errCh <- fmt.Errorf("no authorization code in callback")
		l.HandleErrorRedirect(w)
		return
	}

	l.mu.Lock()
	expectedState := l.expectedState
	l.pendingResp = w
	l.mu.Unlock()

	if expectedState != "" && state != expectedState {
		l.errCh <- fmt.Errorf("state mismatch: expected %s, got %s", expectedState, state)
		l.HandleErrorRedirect(w)
		return
	}

	fmt.Fprintf(w, "<html><body><h2>Authorization successful!</h2><p>You can close this tab.</p></body></html>")

	l.codeCh <- code
}

func shouldUseClaudeAIAuth(scopes []string) bool {
	for _, s := range scopes {
		if s == "user:inference" {
			return true
		}
	}
	return false
}