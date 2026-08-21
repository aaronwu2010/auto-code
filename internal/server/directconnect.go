package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/types"
)

const (
	maxSessionsPerIP = 5
	maxTotalSessions = 100
)

type DirectConnectServer struct {
	mu        sync.RWMutex
	server    *http.Server
	port      int
	authToken string
	sessions  map[string]*DirectConnectSession
}

type DirectConnectSession struct {
	SessionID types.SessionID `json:"session_id"`
	ClientIP  string          `json:"client_ip"`
	Active    bool            `json:"active"`
}

func NewDirectConnectServer(port int, authToken string) *DirectConnectServer {
	return &DirectConnectServer{
		port:      port,
		authToken: authToken,
		sessions:  make(map[string]*DirectConnectSession),
	}
}

func (s *DirectConnectServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session", s.handleSession)
	mux.HandleFunc("/api/health", s.handleHealth)

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("listening on port %d: %w", s.port, err)
	}

	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			return
		}
	}()

	go func() {
		<-ctx.Done()
		s.server.Close()
	}()

	return nil
}

func (s *DirectConnectServer) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

func (s *DirectConnectServer) CreateSession(clientIP string) (*DirectConnectSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.sessions) >= maxTotalSessions {
		return nil, fmt.Errorf("maximum total sessions reached")
	}

	ipCount := 0
	for _, sess := range s.sessions {
		if sess.ClientIP == clientIP {
			ipCount++
		}
	}
	if ipCount >= maxSessionsPerIP {
		return nil, fmt.Errorf("maximum sessions per IP reached")
	}

	session := &DirectConnectSession{
		SessionID: types.SessionID(fmt.Sprintf("dc-%d", len(s.sessions)+1)),
		ClientIP:  clientIP,
		Active:    true,
	}
	s.sessions[string(session.SessionID)] = session
	return session, nil
}

func (s *DirectConnectServer) GetSessions() []*DirectConnectSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*DirectConnectSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		result = append(result, sess)
	}
	return result
}

func (s *DirectConnectServer) handleSession(w http.ResponseWriter, r *http.Request) {
	if s.authToken != "" {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.authToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	clientIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}

	session, err := s.CreateSession(clientIP)
	if err != nil {
		if strings.Contains(err.Error(), "maximum") {
			http.Error(w, err.Error(), http.StatusTooManyRequests)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"session_id":"%s"}`, session.SessionID)
}

func (s *DirectConnectServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}
