package adapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"xanax/internal/config"
	"xanax/internal/session"
)

// errAuth signals that the opencode server rejected our unauthenticated
// request; state watching then stops and the session degrades to
// running/exited (the harness itself keeps working).
var errAuth = errors.New("opencode server requires authentication")

// opencodeAdapter launches the opencode TUI with a bound local HTTP server and
// observes state via its SSE /event stream (SPEC.md §5).
//
// The TUI binds a real HTTP server when given --port; opencode manages its own
// auth for its in-process client, so we must NOT set OPENCODE_SERVER_PASSWORD
// (doing so makes the TUI's own requests 401 and crashes it). We watch the
// event stream unauthenticated; if the server demands auth we simply stop
// watching. The initial prompt is delivered with the TUI's --prompt flag.
type opencodeAdapter struct {
	sess   *session.Session
	h      config.Harness
	logger *slog.Logger

	port int
	base string

	states chan StateEvent
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	closeOnce sync.Once

	mu  sync.Mutex
	ref string
}

func newOpencode(sess *session.Session, h config.Harness, deps Deps) (Adapter, error) {
	ctx, cancel := context.WithCancel(context.Background())
	return &opencodeAdapter{
		sess:   sess,
		h:      h,
		logger: deps.Logger,
		states: make(chan StateEvent, 32),
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

func (a *opencodeAdapter) Launch(resume bool) (LaunchSpec, error) {
	port, err := freePort()
	if err != nil {
		return LaunchSpec{}, fmt.Errorf("allocate opencode port: %w", err)
	}
	a.port = port
	a.base = fmt.Sprintf("http://127.0.0.1:%d", port)

	args := []string{"--hostname", "127.0.0.1", "--port", strconv.Itoa(port)}
	if resume && a.sess.HarnessSessionRef != "" {
		args = append(args, "--session", a.sess.HarnessSessionRef)
	} else if a.sess.InitialPrompt != "" {
		args = append(args, "--prompt", a.sess.InitialPrompt)
	}
	args = append(args, a.h.Args...)

	return LaunchSpec{Path: a.h.Command, Args: args, Env: mergeEnv(a.h.Env), Dir: a.sess.RepoPath}, nil
}

func (a *opencodeAdapter) AfterStart(io.Writer) error {
	a.wg.Add(1)
	go a.watch()
	return nil
}

func (a *opencodeAdapter) States() <-chan StateEvent { return a.states }

func (a *opencodeAdapter) SessionRef() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ref
}

// Close is idempotent: the supervisor calls it both explicitly (to stop the
// state channel before waiting) and via a deferred catch-all, so a second call
// must not re-close a.states.
func (a *opencodeAdapter) Close() error {
	a.closeOnce.Do(func() {
		a.cancel()
		a.wg.Wait()
		close(a.states)
	})
	return nil
}

// watch connects to the SSE stream and republishes normalized state events,
// reconnecting with backoff until the context is cancelled.
func (a *opencodeAdapter) watch() {
	defer a.wg.Done()
	backoff := 200 * time.Millisecond
	for a.ctx.Err() == nil {
		err := a.streamOnce()
		if errors.Is(err, errAuth) {
			a.logger.Info("opencode event stream requires auth; state detection disabled for this session")
			return // harness still works; we just can't observe state
		}
		if err != nil && a.ctx.Err() == nil {
			a.logger.Debug("opencode sse disconnected", "err", err, "backoff", backoff)
			if !sleepCtx(a.ctx, backoff) {
				return
			}
			if backoff < 3*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 200 * time.Millisecond
	}
}

func (a *opencodeAdapter) streamOnce() error {
	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, a.base+"/event", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := (&http.Client{}).Do(req) // streaming: no client timeout
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errAuth
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sse status %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		data, ok := bytes.CutPrefix(sc.Bytes(), []byte("data:"))
		if !ok {
			continue // ignore event:, id:, comments, blank separators
		}
		ev, ref, emit := parseOpencodeEvent(bytes.TrimSpace(data))
		if ref != "" {
			a.mu.Lock()
			if a.ref == "" {
				a.ref = ref
			}
			a.mu.Unlock()
		}
		if !emit {
			continue
		}
		select {
		case a.states <- ev:
		case <-a.ctx.Done():
			return nil
		}
	}
	return sc.Err()
}

// opencodeEvent is the envelope of a bus event on /event:
// {"id": "...", "type": "...", "properties": {...}}.
type opencodeEvent struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

// parseOpencodeEvent maps one SSE data payload to a normalized state event.
// ref is the sessionID when present; emit is false for events that only carry
// a ref or are not state-relevant.
func parseOpencodeEvent(data []byte) (ev StateEvent, ref string, emit bool) {
	var e opencodeEvent
	if err := json.Unmarshal(data, &e); err != nil {
		return StateEvent{}, "", false
	}
	switch e.Type {
	case "session.status":
		var p struct {
			SessionID string `json:"sessionID"`
			Status    struct {
				Type string `json:"type"` // "idle" | "busy" | "retry"
			} `json:"status"`
		}
		_ = json.Unmarshal(e.Properties, &p)
		switch p.Status.Type {
		case "busy", "retry":
			return StateEvent{Kind: StateBusy}, p.SessionID, true
		case "idle":
			return StateEvent{Kind: StateIdle}, p.SessionID, true
		default:
			return StateEvent{}, p.SessionID, false
		}
	case "session.idle":
		var p struct {
			SessionID string `json:"sessionID"`
		}
		_ = json.Unmarshal(e.Properties, &p)
		return StateEvent{Kind: StateIdle}, p.SessionID, true
	case "permission.updated":
		// properties is a Permission object: {id, sessionID, title, ...}.
		var p struct {
			SessionID string `json:"sessionID"`
			Title     string `json:"title"`
		}
		_ = json.Unmarshal(e.Properties, &p)
		return StateEvent{Kind: StateNeedsInput, Message: p.Title}, p.SessionID, true
	case "session.error":
		var p struct {
			SessionID string `json:"sessionID"`
			Error     struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(e.Properties, &p)
		return StateEvent{Kind: StateError, Message: p.Error.Message}, p.SessionID, true
	default:
		return StateEvent{}, "", false
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
