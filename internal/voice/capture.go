package voice

import (
	"context"
	"io"
	"os/exec"
	"runtime"
	"sync"
)

const (
	RecordingSampleRate = 16000
	RecordingChannels   = 1
	SilenceDurationSecs = "2.0"
	SilenceThreshold    = "3%"
)

type RecordingBackend string

const (
	BackendNative  RecordingBackend = "native"
	BackendSoX     RecordingBackend = "sox"
	BackendArecord RecordingBackend = "arecord"
)

type RecordingAvailability struct {
	Available bool
	Reason    string
}

type AudioChunk = []byte

type AudioCapture struct {
	mu         sync.Mutex
	backend    RecordingBackend
	cmd        *exec.Cmd
	cancelFunc context.CancelFunc
	active     bool
	onData     func(chunk AudioChunk)
	onEnd      func()
}

func NewAudioCapture(onData func(chunk AudioChunk), onEnd func()) *AudioCapture {
	return &AudioCapture{
		onData:  onData,
		onEnd:   onEnd,
		backend: detectBackend(),
	}
}

func detectBackend() RecordingBackend {
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("arecord"); err == nil {
			return BackendArecord
		}
	}
	if _, err := exec.LookPath("rec"); err == nil {
		return BackendSoX
	}
	return BackendNative
}

func CheckRecordingAvailability() RecordingAvailability {
	if runtime.GOOS == "windows" {
		return RecordingAvailability{
			Available: false,
			Reason:    "Voice recording requires the native audio module",
		}
	}

	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("arecord"); err == nil {
			return RecordingAvailability{Available: true}
		}
	}

	if _, err := exec.LookPath("rec"); err == nil {
		return RecordingAvailability{Available: true}
	}

	return RecordingAvailability{
		Available: false,
		Reason:    "Voice mode requires SoX for audio recording. Install: brew install sox (macOS) or sudo apt-get install sox (Linux)",
	}
}

func (c *AudioCapture) Start(ctx context.Context, silenceDetection bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.active {
		return false
	}

	captureCtx, cancel := context.WithCancel(ctx)
	c.cancelFunc = cancel

	switch c.backend {
	case BackendArecord:
		return c.startArecord(captureCtx)
	case BackendSoX:
		return c.startSoX(captureCtx, silenceDetection)
	default:
		return false
	}
}

func (c *AudioCapture) startSoX(ctx context.Context, silenceDetection bool) bool {
	args := []string{
		"-q",
		"--buffer", "1024",
		"-t", "raw",
		"-r", "16000",
		"-e", "signed",
		"-b", "16",
		"-c", "1",
		"-",
	}

	if silenceDetection {
		args = append(args,
			"silence", "1", "0.1", SilenceThreshold,
			"1", SilenceDurationSecs, SilenceThreshold,
		)
	}

	cmd := exec.CommandContext(ctx, "rec", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}

	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return false
	}

	c.cmd = cmd
	c.active = true

	go c.readOutput(stdout)
	go c.waitProcess()

	return true
}

func (c *AudioCapture) startArecord(ctx context.Context) bool {
	args := []string{
		"-f", "S16_LE",
		"-r", "16000",
		"-c", "1",
		"-t", "raw",
		"-q",
		"-",
	}

	cmd := exec.CommandContext(ctx, "arecord", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}

	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return false
	}

	c.cmd = cmd
	c.active = true

	go c.readOutput(stdout)
	go c.waitProcess()

	return true
}

func (c *AudioCapture) readOutput(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 && c.onData != nil {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			c.onData(chunk)
		}
		if err != nil {
			return
		}
	}
}

func (c *AudioCapture) waitProcess() {
	if c.cmd != nil {
		_ = c.cmd.Wait()
	}
	c.mu.Lock()
	c.active = false
	c.cmd = nil
	c.mu.Unlock()

	if c.onEnd != nil {
		c.onEnd()
	}
}

func (c *AudioCapture) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancelFunc != nil {
		c.cancelFunc()
		c.cancelFunc = nil
	}

	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	c.active = false
}

func (c *AudioCapture) IsActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

func (c *AudioCapture) GetBackend() RecordingBackend {
	return c.backend
}

type VoiceDependencies struct {
	Available     bool
	Missing       []string
	InstallCmd    string
}

func CheckVoiceDependencies() VoiceDependencies {
	if runtime.GOOS == "windows" {
		return VoiceDependencies{
			Available: false,
			Missing:   []string{"Voice mode requires the native audio module"},
		}
	}

	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("arecord"); err == nil {
			return VoiceDependencies{Available: true}
		}
	}

	if _, err := exec.LookPath("rec"); err == nil {
		return VoiceDependencies{Available: true}
	}

	missing := []string{"sox (rec command)"}
	installCmd := ""

	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("brew"); err == nil {
			installCmd = "brew install sox"
		}
	case "linux":
		if _, err := exec.LookPath("apt-get"); err == nil {
			installCmd = "sudo apt-get install sox"
		} else if _, err := exec.LookPath("dnf"); err == nil {
			installCmd = "sudo dnf install sox"
		}
	}

	return VoiceDependencies{
		Available:  len(missing) == 0,
		Missing:    missing,
		InstallCmd: installCmd,
	}
}