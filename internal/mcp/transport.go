package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/auto-code/auto-code/internal/utils/executil"
)

type Transport interface {
	Send(ctx context.Context, msg *JSONRPCRequest) (*JSONRPCResponse, error)
	SendNotification(ctx context.Context, notif *JSONRPCNotification) error
	Close() error
	OnNotification(handler func(*JSONRPCNotification))
}

type StdioTransport struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Scanner
	stderr  io.Reader
	mu      sync.Mutex
	pending map[any]chan *JSONRPCResponse
	nextID  atomic.Int64
	onNotif func(*JSONRPCNotification)
	closed  bool
	closeCh chan struct{}
	// readDone 用于等待 readLoop goroutine 退出
	readDone chan struct{}
}

func NewStdioTransport(command string, args []string, env map[string]string) (*StdioTransport, error) {
	cmd := executil.Command(command, args...)
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	t := &StdioTransport{
		cmd:      cmd,
		stdin:    stdinPipe,
		stdout:   bufio.NewScanner(stdoutPipe),
		stderr:   stderrPipe,
		pending:  make(map[any]chan *JSONRPCResponse),
		closeCh:  make(chan struct{}),
		readDone: make(chan struct{}),
	}

	go t.readLoop()

	return t, nil
}

func (t *StdioTransport) Send(ctx context.Context, msg *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport closed")
	}

	if msg.ID == nil {
		msg.ID = t.nextID.Add(1)
	}

	ch := make(chan *JSONRPCResponse, 1)
	t.pending[msg.ID] = ch
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.pending, msg.ID)
		t.mu.Unlock()
	}()

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}

	data = append(data, '\n')

	if _, err := t.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write message: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		return resp, nil
	case <-t.closeCh:
		return nil, fmt.Errorf("transport closed")
	}
}

func (t *StdioTransport) SendNotification(ctx context.Context, notif *JSONRPCNotification) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("transport closed")
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	data = append(data, '\n')

	_, err = t.stdin.Write(data)
	return err
}

func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true
	close(t.closeCh)

	for id, ch := range t.pending {
		close(ch)
		delete(t.pending, id)
	}

	_ = t.stdin.Close()
	_ = t.cmd.Process.Kill()
	_ = t.cmd.Wait()

	// 等待 readLoop goroutine 退出，避免泄漏
	<-t.readDone

	return nil
}

func (t *StdioTransport) OnNotification(handler func(*JSONRPCNotification)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onNotif = handler
}

func (t *StdioTransport) readLoop() {
	defer close(t.readDone)

	t.stdout.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for t.stdout.Scan() {
		line := t.stdout.Text()
		if line == "" {
			continue
		}

		msg, err := ParseMessage([]byte(line))
		if err != nil {
			continue
		}

		switch m := msg.(type) {
		case *JSONRPCResponse:
			t.mu.Lock()
			if ch, ok := t.pending[m.ID]; ok {
				ch <- m
			}
			t.mu.Unlock()
		case *JSONRPCNotification:
			t.mu.Lock()
			handler := t.onNotif
			t.mu.Unlock()
			if handler != nil {
				handler(m)
			}
		}
	}
}

type InProcessTransport struct {
	mu      sync.Mutex
	peer    *InProcessTransport
	closed  bool
	onNotif func(*JSONRPCNotification)
	onMsg   func(*JSONRPCResponse)
	// responseChans 存储等待响应的请求
	responseChans map[any]chan *JSONRPCResponse
}

func NewInProcessTransportPair() (*InProcessTransport, *InProcessTransport) {
	a := &InProcessTransport{responseChans: make(map[any]chan *JSONRPCResponse)}
	b := &InProcessTransport{responseChans: make(map[any]chan *JSONRPCResponse)}
	a.peer = b
	b.peer = a
	return a, b
}

func (t *InProcessTransport) Send(ctx context.Context, msg *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	if t.closed || t.peer == nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport closed")
	}

	ch := make(chan *JSONRPCResponse, 1)
	t.responseChans[msg.ID] = ch
	peer := t.peer
	t.mu.Unlock()

	// 将请求转发给 peer 的 onMsg 处理器
	// peer 的 onMsg 处理器应调用 t.SendResponse(id, resp) 来返回响应
	if peer.onMsg != nil {
		peer.onMsg(&JSONRPCResponse{
			JSONRPC: JSONRPCVersion,
			ID:      msg.ID,
			Result:  msg.Params,
		})
	}

	// 等待 peer 通过 SendResponse 返回实际响应
	select {
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.responseChans, msg.ID)
		t.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		return resp, nil
	}
}

// SendResponse 由 peer 的 onMsg 处理器调用，用于将响应路由回请求方
func (t *InProcessTransport) SendResponse(id any, resp *JSONRPCResponse) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ch, ok := t.responseChans[id]; ok {
		ch <- resp
		delete(t.responseChans, id)
	}
}

func (t *InProcessTransport) SendNotification(_ context.Context, notif *JSONRPCNotification) error {
	t.mu.Lock()
	if t.closed || t.peer == nil {
		t.mu.Unlock()
		return fmt.Errorf("transport closed")
	}
	peer := t.peer
	t.mu.Unlock()

	if peer.onNotif != nil {
		peer.onNotif(notif)
	}
	return nil
}

func (t *InProcessTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	if t.peer != nil {
		t.peer.closed = true
	}
	return nil
}

func (t *InProcessTransport) OnNotification(handler func(*JSONRPCNotification)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onNotif = handler
}
