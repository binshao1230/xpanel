package xrayproc

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Manager starts, stops and restarts an Xray core process.
type Manager struct {
	mu         sync.Mutex
	bin        string
	configPath string
	cmd        *exec.Cmd
	startedAt  time.Time
	lastErr    string
	logLines   []string
	maxLog     int
	// waitDone is closed when the current process exits.
	waitDone chan struct{}
}

func New(bin, configPath string) *Manager {
	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	if abs, err := filepath.Abs(bin); err == nil && fileExists(abs) {
		bin = abs
	}
	return &Manager{
		bin:        bin,
		configPath: configPath,
		maxLog:     300,
		logLines:   make([]string, 0, 64),
	}
}

func (m *Manager) Bin() string    { return m.bin }
func (m *Manager) Config() string { return m.configPath }

func (m *Manager) SetBin(bin string) {
	m.mu.Lock()
	m.bin = bin
	m.mu.Unlock()
}

// ResolveBin finds an xray binary from candidates.
func ResolveBin(explicit string, extraDirs ...string) string {
	cands := []string{}
	if explicit != "" {
		cands = append(cands, explicit)
	}
	cands = append(cands, "xray")
	if runtime.GOOS == "windows" {
		cands = append(cands, "xray.exe")
	}
	for _, d := range extraDirs {
		if d == "" {
			continue
		}
		cands = append(cands,
			filepath.Join(d, "xray"),
			filepath.Join(d, "xray.exe"),
			filepath.Join(d, "bin", "xray"),
			filepath.Join(d, "bin", "xray.exe"),
		)
	}
	// look next to current executable
	if exePath, err := os.Executable(); err == nil {
		dir := filepath.Dir(exePath)
		cands = append(cands,
			filepath.Join(dir, "xray"),
			filepath.Join(dir, "xray.exe"),
			filepath.Join(dir, "bin", "xray"),
			filepath.Join(dir, "bin", "xray.exe"),
		)
	}
	seen := map[string]bool{}
	for _, c := range cands {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		// absolute or relative file
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			if abs, err := filepath.Abs(c); err == nil {
				return abs
			}
			return c
		}
		// PATH lookup for bare names
		if !strings.ContainsAny(c, `/\`) {
			if p, err := exec.LookPath(c); err == nil {
				return p
			}
		}
	}
	return explicit
}

func (m *Manager) Available() bool {
	m.mu.Lock()
	bin := m.bin
	m.mu.Unlock()
	if bin == "" {
		return false
	}
	st, err := os.Stat(bin)
	return err == nil && !st.IsDir()
}

func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runningLocked()
}

func (m *Manager) runningLocked() bool {
	return m.cmd != nil && m.cmd.Process != nil && m.cmd.ProcessState == nil
}

func (m *Manager) UptimeSec() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.runningLocked() {
		return 0
	}
	return int64(time.Since(m.startedAt).Seconds())
}

func (m *Manager) LastError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}

func (m *Manager) Logs(n int) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n <= 0 || n > len(m.logLines) {
		n = len(m.logLines)
	}
	from := len(m.logLines) - n
	out := make([]string, n)
	copy(out, m.logLines[from:])
	return out
}

// ApplyConfigBytes writes config after -test validation, then restarts xray.
// If validation fails, the previous config and process are left untouched.
func (m *Manager) ApplyConfigBytes(raw []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(m.configPath), 0o755); err != nil {
		return err
	}
	// Xray detects format by file extension — must end with .json (not .tmp).
	// Always use absolute paths: xray process Dir is binary folder.
	cfgAbs := m.configPath
	if abs, err := filepath.Abs(cfgAbs); err == nil {
		cfgAbs = abs
		m.configPath = abs
	}
	tmp := cfgAbs + ".apply.json"
	if err := os.MkdirAll(filepath.Dir(cfgAbs), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	defer os.Remove(tmp)

	if err := m.testFileLocked(tmp); err != nil {
		m.lastErr = err.Error()
		return err
	}

	// replace target config
	if err := os.WriteFile(cfgAbs, raw, 0o644); err != nil {
		m.lastErr = err.Error()
		return err
	}

	_ = m.stopLocked()
	if err := m.startLocked(); err != nil {
		return err
	}
	return nil
}

// EnsureRunning starts xray if a valid config exists and process is down.
func (m *Manager) EnsureRunning() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runningLocked() {
		return nil
	}
	if _, err := os.Stat(m.configPath); err != nil {
		return fmt.Errorf("no config: %w", err)
	}
	return m.startLocked()
}

func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runningLocked() {
		return nil
	}
	return m.startLocked()
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked()
}

func (m *Manager) Restart() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.stopLocked()
	return m.startLocked()
}

func (m *Manager) testFileLocked(path string) error {
	if !fileExists(m.bin) {
		return fmt.Errorf("xray binary not found: %s", m.bin)
	}
	absPath := path
	if a, err := filepath.Abs(path); err == nil {
		absPath = a
	}
	cmd := exec.Command(m.bin, "run", "-test", "-c", absPath)
	// keep geo files discoverable next to binary
	if dir := filepath.Dir(m.bin); dir != "" && dir != "." {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("xray -test failed: %s", msg)
	}
	return nil
}

func (m *Manager) startLocked() error {
	if !fileExists(m.bin) {
		err := fmt.Errorf("xray binary not found: %s", m.bin)
		m.lastErr = err.Error()
		return err
	}
	cfgAbs := m.configPath
	if a, err := filepath.Abs(cfgAbs); err == nil {
		cfgAbs = a
		m.configPath = a
	}
	if _, err := os.Stat(cfgAbs); err != nil {
		return fmt.Errorf("config not found: %w", err)
	}
	if err := m.testFileLocked(cfgAbs); err != nil {
		m.lastErr = err.Error()
		return err
	}

	cmd := exec.Command(m.bin, "run", "-c", cfgAbs)
	if dir := filepath.Dir(m.bin); dir != "" && dir != "." {
		cmd.Dir = dir
	}
	// inherit nothing special; capture logs
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		m.lastErr = err.Error()
		return err
	}
	m.cmd = cmd
	m.startedAt = time.Now()
	m.lastErr = ""
	done := make(chan struct{})
	m.waitDone = done

	go m.consume(stdout)
	go m.consume(stderr)

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case err := <-waitCh:
		m.cmd = nil
		close(done)
		msg := "xray exited immediately"
		if err != nil {
			msg = fmt.Sprintf("xray exited immediately: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
		if logs := m.snapshotLogs(8); len(logs) > 0 {
			msg += " | " + strings.Join(logs, " ; ")
		}
		m.lastErr = msg
		return fmt.Errorf("%s", msg)
	case <-time.After(400 * time.Millisecond):
		go func() {
			err := <-waitCh
			m.mu.Lock()
			if m.cmd == cmd {
				m.cmd = nil
				if err != nil && m.lastErr == "" {
					m.lastErr = fmt.Sprintf("xray exited: %v", err)
				}
			}
			close(done)
			m.mu.Unlock()
		}()
	}
	return nil
}

func (m *Manager) stopLocked() error {
	if m.cmd == nil || m.cmd.Process == nil {
		m.cmd = nil
		return nil
	}
	proc := m.cmd.Process
	done := m.waitDone
	var err error
	if runtime.GOOS == "windows" {
		err = proc.Kill()
	} else {
		_ = proc.Signal(os.Interrupt)
		if done != nil {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				err = proc.Kill()
			}
		} else {
			time.Sleep(500 * time.Millisecond)
			_ = proc.Kill()
		}
	}
	// wait a bit for wait goroutine
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
	m.cmd = nil
	return err
}

func (m *Manager) consume(r io.Reader) {
	sc := bufio.NewScanner(r)
	// long lines from xray are rare but possible
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		m.mu.Lock()
		m.logLines = append(m.logLines, line)
		if len(m.logLines) > m.maxLog {
			m.logLines = m.logLines[len(m.logLines)-m.maxLog:]
		}
		m.mu.Unlock()
	}
}

func (m *Manager) snapshotLogs(n int) []string {
	if n > len(m.logLines) {
		n = len(m.logLines)
	}
	if n == 0 {
		return nil
	}
	from := len(m.logLines) - n
	out := make([]string, n)
	copy(out, m.logLines[from:])
	return out
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
