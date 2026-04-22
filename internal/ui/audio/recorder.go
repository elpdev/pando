package audio

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

type Recorder struct {
	mu        sync.Mutex
	recording bool
	active    *recordingProcess
	path      string
	cleanup   func()
	stopOnce  func()
	closed    bool
}

type recordingProcess struct {
	cmd  *exec.Cmd
	path string
	mu   sync.Mutex
	wg   sync.WaitGroup
	err  error
}

type recordingCandidate struct {
	name string
	args func(path string) []string
}

func NewRecorder() *Recorder {
	return &Recorder{}
}

func (r *Recorder) Start() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("voice recorder is closed")
	}
	if r.recording {
		r.mu.Unlock()
		return fmt.Errorf("voice recording already in progress")
	}
	r.mu.Unlock()

	path, cleanup, err := createRecordingFile()
	if err != nil {
		return err
	}
	cmd, err := recordCommandFor(path)
	if err != nil {
		cleanup()
		return err
	}
	if err := cmd.Start(); err != nil {
		cleanup()
		return fmt.Errorf("start voice recording: %w", err)
	}

	proc := &recordingProcess{cmd: cmd, path: path}
	proc.wg.Add(1)
	go func() {
		defer proc.wg.Done()
		proc.mu.Lock()
		proc.err = cmd.Wait()
		proc.mu.Unlock()
	}()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		r.mu.Unlock()
		proc.kill()
		cleanup()
		r.mu.Lock()
		return fmt.Errorf("voice recorder is closed")
	}
	r.active = proc
	r.path = path
	r.cleanup = cleanup
	r.recording = true
	r.stopOnce = onceFunc(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.active = nil
		r.recording = false
		r.stopOnce = nil
	})
	return nil
}

func (r *Recorder) Stop() (string, error) {
	r.mu.Lock()
	proc := r.active
	path := r.path
	stopOnce := r.stopOnce
	r.mu.Unlock()
	if proc == nil {
		return "", fmt.Errorf("no voice recording in progress")
	}
	if err := proc.stop(); err != nil {
		if stopOnce != nil {
			stopOnce()
		}
		return "", err
	}
	if stopOnce != nil {
		stopOnce()
	}
	return path, nil
}

func (r *Recorder) Cancel() error {
	r.mu.Lock()
	proc := r.active
	cleanup := r.cleanup
	r.cleanup = nil
	r.path = ""
	stopOnce := r.stopOnce
	r.mu.Unlock()
	if proc == nil {
		return nil
	}
	if err := proc.kill(); err != nil {
		if stopOnce != nil {
			stopOnce()
		}
		return err
	}
	if cleanup != nil {
		cleanup()
	}
	if stopOnce != nil {
		stopOnce()
	}
	return nil
}

func (r *Recorder) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return r.Cancel()
}

func (r *Recorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recording
}

func createRecordingFile() (string, func(), error) {
	dir, err := os.MkdirTemp("", "pando-voice-recording-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp voice note: %w", err)
	}
	path := filepath.Join(dir, "voice-note.wav")
	tmp, err := os.Create(path)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("create temp voice file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("close temp voice note: %w", err)
	}
	return path, onceFunc(func() { _ = os.RemoveAll(dir) }), nil
}

func recordCommandFor(path string) (*exec.Cmd, error) {
	candidates := recordingCandidates(runtime.GOOS)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("voice recording is not supported on %s", runtime.GOOS)
	}
	tried := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate.name); err != nil {
			tried = append(tried, candidate.name)
			continue
		}
		return exec.Command(candidate.name, candidate.args(path)...), nil
	}
	return nil, fmt.Errorf("no supported voice recorder found; install one of: %s", strings.Join(uniqueStrings(tried), ", "))
}

func recordingCandidates(goos string) []recordingCandidate {
	switch goos {
	case "linux":
		return []recordingCandidate{
			{name: "pw-record", args: func(path string) []string { return []string{"--rate", "16000", "--channels", "1", path} }},
			{name: "ffmpeg", args: func(path string) []string {
				return []string{"-hide_banner", "-loglevel", "error", "-f", "pulse", "-i", "default", "-ac", "1", "-ar", "16000", "-y", path}
			}},
			{name: "arecord", args: func(path string) []string { return []string{"-q", "-f", "S16_LE", "-c", "1", "-r", "16000", path} }},
		}
	case "darwin":
		return []recordingCandidate{
			{name: "ffmpeg", args: func(path string) []string {
				return []string{"-hide_banner", "-loglevel", "error", "-f", "avfoundation", "-i", ":default", "-ac", "1", "-ar", "16000", "-y", path}
			}},
			{name: "rec", args: func(path string) []string { return []string{"-q", "-c", "1", "-r", "16000", path} }},
			{name: "sox", args: func(path string) []string { return []string{"-q", "-d", "-c", "1", "-r", "16000", path} }},
		}
	default:
		return nil
	}
}

func (p *recordingProcess) stop() error {
	if p == nil {
		return nil
	}
	if p.cmd == nil || p.cmd.Process == nil {
		return fmt.Errorf("voice recorder process not available")
	}
	if err := signalProcess(p.cmd.Process, os.Interrupt, syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop voice recording: %w", err)
	}
	p.wg.Wait()
	if err := p.waitErr(); err != nil && !isAcceptableStopError(err) && !hasRecordedAudio(p.path) {
		return fmt.Errorf("stop voice recording: %w", err)
	}
	return nil
}

func (p *recordingProcess) kill() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	if err := p.cmd.Process.Kill(); err != nil && !strings.Contains(err.Error(), "process already finished") {
		return fmt.Errorf("cancel voice recording: %w", err)
	}
	p.wg.Wait()
	return nil
}

func (p *recordingProcess) waitErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func isAcceptableStopError(err error) bool {
	if err == nil {
		return true
	}
	text := err.Error()
	for _, needle := range []string{"signal: interrupt", "signal: terminated", "terminated by signal", "received signal", "exit status 255"} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func signalProcess(proc *os.Process, signals ...os.Signal) error {
	if proc == nil {
		return fmt.Errorf("voice recorder process not available")
	}
	var lastErr error
	for _, signal := range signals {
		if signal == nil {
			continue
		}
		if err := proc.Signal(signal); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no stop signal available")
	}
	return lastErr
}

func hasRecordedAudio(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

func RecordedFilename(path string) string {
	base := filepath.Base(strings.TrimSpace(path))
	if base == "" || base == "." {
		return "voice-note.wav"
	}
	return base
}
