// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

var ErrBusy = errors.New("model is in use")
var ErrStopped = errors.New("watchdog has stopped")
var ErrBackendIdentity = errors.New("backend identity does not match this startup")
var ErrUnknownModel = errors.New("unknown model")

type Snapshot struct {
	Status           string  `json:"status"`
	CurrentModel     string  `json:"current_model"`
	CurrentModelID   string  `json:"current_model_id"`
	BackendType      string  `json:"backend_type"`
	IsTranslation    bool    `json:"is_translation"`
	IdleTimeout      int     `json:"idle_timeout"`
	IdleRemaining    int     `json:"idle_remaining"`
	AutoSleepEnabled bool    `json:"auto_sleep_enabled"`
	ActiveRequests   int     `json:"active_requests"`
	RequestCount     uint64  `json:"request_count"`
	Models           []Model `json:"models"`
}

type Manager struct {
	cfg                        Config
	mu                         sync.Mutex
	gate                       chan struct{}
	changed                    chan struct{}
	ctx                        context.Context
	cancel                     context.CancelFunc
	models                     []Model
	current                    Model
	proc                       *child
	starting, ready, autoSleep bool
	lastActive                 time.Time
	active                     int
	requests                   uint64
	start                      func(context.Context, Model) (*child, error)
	probe                      func(context.Context, Model) error
}

func NewManager(c Config) (*Manager, error) {
	if err := validateConfig(c); err != nil {
		return nil, err
	}
	models, err := Discover(c)
	if err != nil {
		return nil, err
	}
	last, err := LoadLastModel(c.DataDir)
	if err != nil {
		return nil, fmt.Errorf("read last model: %w", err)
	}
	return managerForModels(c, models, last), nil
}

func managerForModels(c Config, models []Model, last string) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{cfg: c, models: models, gate: make(chan struct{}, 1), changed: make(chan struct{}), ctx: ctx, cancel: cancel,
		autoSleep: true, lastActive: time.Now()}
	if model, ok := FindModel(models, last); last != "" && ok {
		m.current = model
	} else if len(models) > 0 {
		m.current = models[0]
	}
	m.start = func(ctx context.Context, model Model) (*child, error) { return startProcess(ctx, c, model) }
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	m.probe = func(ctx context.Context, model Model) error {
		request, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://127.0.0.1:%d%s", c.InternalPort, HealthPath(model)), nil)
		if err != nil {
			return err
		}
		request.Close = true
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 4096)); err != nil {
			return err
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("backend health HTTP %d", response.StatusCode)
		}
		return nil
	}
	return m
}

func (m *Manager) acquire(ctx context.Context, wait bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.ctx.Err() != nil {
		return ErrStopped
	}
	if !wait {
		select {
		case m.gate <- struct{}{}:
			return nil
		default:
			return ErrBusy
		}
	}
	ctx, cancel := context.WithTimeout(ctx, m.cfg.QueueTimeout)
	defer cancel()
	select {
	case m.gate <- struct{}{}:
		if m.ctx.Err() != nil {
			<-m.gate
			return ErrStopped
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
		return ErrStopped
	}
}

func (m *Manager) release() { <-m.gate }

// The gate protects lifecycle changes and admission, not response streaming.
// changed is closed under mu when a response releases its model lease.
func (m *Manager) waitForChange(ctx context.Context, changed <-chan struct{}) error {
	select {
	case <-m.ctx.Done():
		return ErrStopped
	case <-ctx.Done():
		return ctx.Err()
	case <-changed:
		return nil
	}
}

func (m *Manager) acquireIdle(ctx context.Context, wait bool) error {
	ctx, cancel := context.WithTimeout(ctx, m.cfg.QueueTimeout)
	defer cancel()
	for {
		if err := m.acquire(ctx, wait); err != nil {
			return err
		}
		m.mu.Lock()
		active, changed := m.active, m.changed
		m.mu.Unlock()
		if active == 0 {
			return nil
		}
		m.release()
		if !wait {
			return ErrBusy
		}
		if err := m.waitForChange(ctx, changed); err != nil {
			return err
		}
	}
}

func modelConcurrency(model Model) int {
	if model.Type == "apple" || model.Type == "apple-api" {
		return 2
	}
	return 1
}

func (m *Manager) stopModel() error {
	m.mu.Lock()
	proc := m.proc
	m.ready = false
	m.mu.Unlock()
	if proc != nil {
		if err := proc.stop(); err != nil {
			return err
		}
		m.mu.Lock()
		if m.proc == proc {
			m.proc = nil
		}
		m.mu.Unlock()
	}
	return nil
}

// wakeLocked is called only with the request gate held. All callers share the
// same startup and readiness check; a PID alone never makes a model ready.
func (m *Manager) wakeLocked(ctx context.Context) error {
	if m.ctx.Err() != nil {
		return ErrStopped
	}
	m.mu.Lock()
	model := m.current
	if m.ready && m.proc != nil {
		select {
		case <-m.proc.done:
			m.ready = false
		default:
			m.lastActive = time.Now()
			m.mu.Unlock()
			return nil
		}
	}
	if m.active > 0 {
		m.mu.Unlock()
		return errors.New("backend stopped during active requests")
	}
	m.starting = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.starting = false; m.mu.Unlock() }()
	if model.ID == "" {
		return errors.New("no available model")
	}
	timeout := m.cfg.StartupTimeout
	if model.Type != "gguf" && model.Type != "laya" {
		timeout = min(timeout, 25*time.Second)
	}
	startup, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	if err := m.stopModel(); err != nil {
		return err
	}
	proc, err := m.start(startup, model)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.proc = proc
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return errors.Join(ErrStopped, m.stopModel())
	}
	m.mu.Unlock()
	ticker := time.NewTicker(m.cfg.ProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-proc.done:
			return errors.Join(fmt.Errorf("model exited before readiness: %v", proc.err), m.stopModel())
		case <-startup.Done():
			return errors.Join(startup.Err(), m.stopModel())
		case <-m.ctx.Done():
			return errors.Join(ErrStopped, m.stopModel())
		default:
		}
		probeError := m.probe(startup, model)
		if errors.Is(probeError, ErrBackendIdentity) {
			return errors.Join(probeError, m.stopModel())
		}
		if probeError == nil {
			m.mu.Lock()
			if m.ctx.Err() != nil || m.proc != proc {
				m.mu.Unlock()
				return ErrStopped
			}
			select {
			case <-proc.done:
				m.mu.Unlock()
				return errors.Join(errors.New("model exited at readiness"), m.stopModel())
			default:
			}
			m.ready, m.lastActive = true, time.Now()
			m.mu.Unlock()
			if err := SaveLastModel(m.cfg.DataDir, model.ID); err != nil {
				return errors.Join(fmt.Errorf("save last model: %w", err), m.stopModel())
			}
			return nil
		}
		select {
		case <-ticker.C:
		case <-startup.Done():
		case <-m.ctx.Done():
		}
	}
}

func (m *Manager) Wake(ctx context.Context) error {
	if err := m.acquireIdle(ctx, true); err != nil {
		return err
	}
	defer m.release()
	return m.wakeLocked(ctx)
}

func (m *Manager) Sleep(ctx context.Context) error {
	if err := m.acquireIdle(ctx, false); err != nil {
		return err
	}
	defer m.release()
	return m.stopModel()
}

func (m *Manager) selectModel(alias string) error {
	if alias == "" || alias == "default" {
		return nil
	}
	m.mu.Lock()
	model, ok := FindModel(m.models, alias)
	changed := ok && model.ID != m.current.ID
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownModel, alias)
	}
	if changed {
		if err := m.stopModel(); err != nil {
			return err
		}
		m.mu.Lock()
		m.current = model
		m.mu.Unlock()
	}
	return nil
}

func (m *Manager) Switch(ctx context.Context, alias string) error {
	if err := m.acquireIdle(ctx, false); err != nil {
		return err
	}
	defer m.release()
	if err := m.selectModel(alias); err != nil {
		return err
	}
	return m.wakeLocked(ctx)
}

// WithModel leases a model for the entire response, including SSE. Apple
// requests may overlap; model switches and shutdown still respect every lease.
// The callback must propagate ctx to its upstream HTTP request.
func (m *Manager) WithModel(ctx context.Context, alias string, send func(context.Context, Model) error) error {
	queue, cancelQueue := context.WithTimeout(ctx, m.cfg.QueueTimeout)
	defer cancelQueue()
	var model Model
	for {
		if err := m.acquire(queue, true); err != nil {
			return err
		}
		m.mu.Lock()
		model = m.current
		same := alias == "" || alias == "default"
		if !same {
			target, ok := FindModel(m.models, alias)
			if !ok {
				m.mu.Unlock()
				m.release()
				return fmt.Errorf("%w: %s", ErrUnknownModel, alias)
			}
			same = target.ID == model.ID
		}
		active, changed := m.active, m.changed
		m.mu.Unlock()
		if active == 0 || (same && active < modelConcurrency(model)) {
			break
		}
		m.release()
		if err := m.waitForChange(queue, changed); err != nil {
			return err
		}
	}
	if err := m.selectModel(alias); err != nil {
		m.release()
		return err
	}
	if err := m.wakeLocked(queue); err != nil {
		m.release()
		return err
	}
	if err := queue.Err(); err != nil {
		m.release()
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	defer cancel()
	m.mu.Lock()
	m.active++
	m.requests++
	model = m.current
	m.mu.Unlock()
	m.release()
	cancelQueue()
	defer func() {
		m.mu.Lock()
		m.active--
		m.lastActive = time.Now()
		close(m.changed)
		m.changed = make(chan struct{})
		m.mu.Unlock()
	}()
	return send(ctx, model)
}

func (m *Manager) SetAutoSleep(enabled *bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if enabled == nil {
		m.autoSleep = !m.autoSleep
	} else {
		m.autoSleep = *enabled
	}
	return m.autoSleep
}

func (m *Manager) Refresh() error {
	models, err := Discover(m.cfg)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.models = models
	if m.current.ID == "" && len(models) > 0 {
		m.current = models[0]
	}
	return nil
}

// CheckIdle is driven by the server's owned ticker; no hidden goroutine.
func (m *Manager) CheckIdle() error {
	if err := m.acquireIdle(context.Background(), false); err != nil {
		if errors.Is(err, ErrBusy) || errors.Is(err, ErrStopped) {
			return nil
		}
		return err
	}
	defer m.release()
	m.mu.Lock()
	idle := m.autoSleep && m.ready && !m.starting && m.active == 0 &&
		m.current.IdleTimeout > 0 && time.Since(m.lastActive) >= time.Duration(m.current.IdleTimeout)*time.Second
	m.mu.Unlock()
	if idle {
		return m.stopModel()
	}
	return nil
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{Status: "sleeping", CurrentModel: m.current.Name, CurrentModelID: m.current.ID,
		BackendType: m.current.Type, IsTranslation: m.current.IsTranslation, IdleTimeout: m.current.IdleTimeout,
		AutoSleepEnabled: m.autoSleep, ActiveRequests: m.active, RequestCount: m.requests,
		Models: append([]Model{}, m.models...)}
	alive := false
	if m.proc != nil {
		select {
		case <-m.proc.done:
		default:
			alive = true
		}
	}
	if m.starting {
		s.Status = "starting"
	} else if m.ready && alive {
		s.Status = "running"
		s.IdleRemaining = max(0, s.IdleTimeout-int(time.Since(m.lastActive).Seconds()))
	}
	return s
}

func (m *Manager) Stop() error {
	m.cancel() // Cancel both active upstream contexts and every queued request.
	err := m.stopModel()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case m.gate <- struct{}{}:
			m.mu.Lock()
			active, changed := m.active, m.changed
			m.mu.Unlock()
			if active == 0 {
				defer m.release()
				return errors.Join(err, m.stopModel())
			}
			m.release()
			select {
			case <-changed:
			case <-timer.C:
				return errors.Join(err, errors.New("active startup/request did not stop after cancellation"))
			}
		case <-timer.C:
			return errors.Join(err, errors.New("active startup/request did not stop after cancellation"))
		}
	}
}
