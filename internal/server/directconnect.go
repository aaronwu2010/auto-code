package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/auto-code/auto-code/internal/types"
)

type DirectConnectServer struct {
	mu       sync.RWMutex
	server   *http.Server
	port     int
	sessions map[string]*DirectConnectSession
}

type DirectConnectSession struct {
	SessionID types.SessionID `json:"session_id"`
	ClientIP  string          `json:"client_ip"`
	Active    bool            `json:"active"`
}

func NewDirectConnectServer(port int) *DirectConnectServer {
	return &DirectConnectServer{
		port:     port,
		sessions: make(map[string]*DirectConnectSession),
	}
}

func (s *DirectConnectServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session", s.handleSession)
	mux.HandleFunc("/api/health", s.handleHealth)

	s.server = &http.Server{
		Handler: mux,
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
	session, err := s.CreateSession(r.RemoteAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"session_id":"%s"}`, session.SessionID)
}

func (s *DirectConnectServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}