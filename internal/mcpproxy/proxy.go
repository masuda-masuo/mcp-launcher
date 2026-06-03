// Package mcpproxy implements an idle-gated, re-handshaking JSON-RPC proxy that
// sits between an MCP client (e.g. Claude Desktop) and a restartable child MCP
// server spoken over stdio.
//
// Why this exists: a transparent stdin/stdout pipe cannot survive a child
// restart. The MCP lifecycle requires initialize -> result ->
// notifications/initialized before the server is operational; a client only
// performs that handshake once per stream. When the launcher kills and respawns
// the child, the client's stream is still alive, so it never re-initializes and
// the new child stays stuck in "initializing", rejecting tools/* calls.
//
// The proxy fixes this by interpreting frames: it caches the client's
// initialize request and notifications/initialized, and on every restart replays
// them to the new child itself (using a sentinel id whose response is swallowed),
// so the client never knows a restart happened.
//
// Restarts are idle-gated. A restart reason (token near expiry, or a rotated
// keystore token) only flags intent; the actual kill waits until in-flight ==
// 0. Requests that arrive during the restart window are queued and flushed to
// the new child afterwards, so they are delayed but never lost. A drain timeout
// bounds the wait: on expiry, in-flight requests receive a retryable error and
// the restart is forced.
//
// Observable states (Starting -> Ready -> Draining -> Restarting -> Ready) exist
// for logging and to make the "queue vs forward" decision a single predicate;
// all transitions go through one mutex so the concurrent reader/writer
// goroutines share one source of truth.
package mcpproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

const (
	// handshakeTimeout bounds how long we wait for a freshly spawned child to
	// answer the replayed (sentinel) initialize request.
	handshakeTimeout = 30 * time.Second
	// spawnBackoff is the pause before retrying a failed spawn/handshake.
	spawnBackoff = 500 * time.Millisecond
	// retryableErrorCode is returned to in-flight requests abandoned by a forced
	// restart, signalling the client may safely retry (for idempotent calls).
	retryableErrorCode = -32000
)

// State is the proxy's externally observable lifecycle state.
type State int

const (
	StateStarting State = iota
	StateReady
	StateDraining
	StateRestarting
)

func (s State) String() string {
	switch s {
	case StateStarting:
		return "Starting"
	case StateReady:
		return "Ready"
	case StateDraining:
		return "Draining"
	case StateRestarting:
		return "Restarting"
	default:
		return "Unknown"
	}
}

// Child is a running child MCP server exposing stdio.
type Child interface {
	// Stdin is where the proxy writes frames destined for the child.
	Stdin() io.Writer
	// Stdout is where the proxy reads frames produced by the child.
	Stdout() io.Reader
	// Kill terminates the process and waits for it to exit. Killing must cause
	// Stdout to reach EOF so the child reader goroutine unwinds.
	Kill() error
}

// Options configures a Proxy. Spawn, Refresh and RestartReason are injected so
// the package stays free of keystore/refresher dependencies and is unit-testable
// with in-process fakes.
type Options struct {
	ClientIn  io.Reader // typically os.Stdin
	ClientOut io.Writer // typically os.Stdout

	// Spawn starts a new child using the current environment and returns it.
	Spawn func(ctx context.Context) (Child, error)
	// Refresh runs immediately before each re-spawn during a restart cycle
	// (e.g. token refresh).  It is NOT called before the initial spawn so that
	// the MCP client's initialize handshake is never blocked on a token fetch
	// at cold-start time (see issue #14).  May be nil.
	Refresh func(ctx context.Context) error
	// RestartReason reports whether a restart is currently warranted and a short
	// human-readable reason. Polled every CheckInterval. May be nil (never
	// restarts on a schedule; crash recovery still applies).
	RestartReason func() (bool, string)

	// CheckInterval is how often RestartReason is polled (restart_interval_seconds).
	CheckInterval time.Duration
	// DrainTimeout bounds the wait for in-flight to reach zero before forcing a
	// restart. Zero means wait indefinitely.
	DrainTimeout time.Duration

	// Logf logs diagnostics (to stderr). May be nil.
	Logf func(format string, args ...any)
}

// Proxy multiplexes a client stream onto a restartable child MCP server.
type Proxy struct {
	opts Options

	mu                sync.Mutex
	state             State
	child             Child
	inFlight          map[string]json.RawMessage // idKey -> raw id (client-originated requests awaiting reply)
	queue             [][]byte                   // frames buffered while draining/restarting
	swallow           map[string]bool            // idKeys whose child responses must be dropped
	handshakeCaptured bool
	initReq           []byte // cached initialize request (original id)
	initNote          []byte // cached notifications/initialized
	protoVersion      string // protocolVersion seen on the first initialize response
	handshakeDone     chan struct{}

	idleCh  chan struct{} // childReader -> control: in-flight emptied while draining
	crashCh chan string   // childReader -> control: current child exited unexpectedly

	clientWriteMu sync.Mutex // serialises writes to ClientOut (one frame at a time)
	childWriteMu  sync.Mutex // serialises writes to the child stdin
}

// New constructs a Proxy.
func New(opts Options) *Proxy {
	return &Proxy{
		opts:     opts,
		state:    StateStarting,
		inFlight: make(map[string]json.RawMessage),
		swallow:  make(map[string]bool),
		idleCh:   make(chan struct{}, 1),
		crashCh:  make(chan string, 1),
	}
}

func (p *Proxy) logf(format string, args ...any) {
	if p.opts.Logf != nil {
		p.opts.Logf(format, args...)
	}
}

// State returns the current observable state (for tests / introspection).
func (p *Proxy) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Run spawns the initial child and serves until the client stream ends or ctx
// is cancelled.  Refresh (if set) is intentionally NOT called here: the caller
// is responsible for any best-effort warm-up before Run, and the restart cycle
// in maybeRestart calls Refresh before every subsequent spawn.
func (p *Proxy) Run(ctx context.Context) error {
	child, err := p.opts.Spawn(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.child = child
	p.state = StateStarting
	p.mu.Unlock()

	go p.childReader(child)
	go p.controlLoop(ctx)

	clientErr := make(chan error, 1)
	go func() { clientErr <- p.clientReader() }()

	select {
	case <-ctx.Done():
		p.shutdown()
		return ctx.Err()
	case err := <-clientErr:
		p.shutdown()
		return err
	}
}

func (p *Proxy) shutdown() {
	p.mu.Lock()
	child := p.child
	p.mu.Unlock()
	if child != nil {
		_ = child.Kill()
	}
}

// ---- client -> child path -------------------------------------------------

func (p *Proxy) clientReader() error {
	r := bufio.NewReader(p.opts.ClientIn)
	for {
		raw, err := readFrame(r)
		if raw != nil {
			p.routeFromClient(raw)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (p *Proxy) routeFromClient(raw []byte) {
	msg, perr := Parse(raw)
	if perr != nil {
		// Unparseable frame: forward verbatim when not paused, else queue.
		p.forwardOrQueue(raw, false, nil)
		return
	}

	switch {
	case msg.Method() == "initialize" && msg.IsRequest():
		// Cache the very first initialize so we can replay it on restart, then
		// forward to the current child as normal.
		p.mu.Lock()
		if p.initReq == nil {
			p.initReq = clone(raw)
		}
		p.mu.Unlock()
		p.forwardOrQueue(raw, false, nil) // not tracked: its response flows back normally

	case msg.Method() == "notifications/initialized" && msg.IsNotification():
		p.mu.Lock()
		if p.initReq != nil {
			p.initNote = clone(raw)
			p.handshakeCaptured = true
			if p.state == StateStarting {
				p.state = StateReady
				p.logf("state -> Ready (handshake captured)")
			}
		}
		p.mu.Unlock()
		p.forwardOrQueue(raw, false, nil)

	default:
		track := msg.IsRequest()
		var id json.RawMessage
		if track {
			id = msg.IDRaw()
		}
		p.forwardOrQueue(raw, track, id)
	}
}

// forwardOrQueue forwards raw to the current child when serving, or buffers it
// while draining/restarting. When track is true the request id is recorded as
// in-flight at the moment it is actually written to the child.
func (p *Proxy) forwardOrQueue(raw []byte, track bool, id json.RawMessage) {
	p.mu.Lock()
	if p.state == StateDraining || p.state == StateRestarting {
		p.queue = append(p.queue, clone(raw))
		p.mu.Unlock()
		return
	}
	if track {
		p.inFlight[string(id)] = id
	}
	child := p.child
	p.mu.Unlock()
	if child != nil {
		if err := p.writeChild(child, raw); err != nil {
			p.logf("warning: write to child failed: %v", err)
		}
	}
}

// ---- child -> client path -------------------------------------------------

// childReader pumps one child's stdout to the client until that child's stream
// ends. A fresh childReader is started for every spawned child.
func (p *Proxy) childReader(child Child) {
	r := bufio.NewReader(child.Stdout())
	for {
		raw, err := readFrame(r)
		if raw != nil {
			p.routeFromChild(raw)
		}
		if err != nil {
			break
		}
	}
	// Stream ended. If this child is still the active one and we are serving,
	// the exit was unexpected (a crash) and warrants a restart.
	p.mu.Lock()
	unexpected := p.child == child && (p.state == StateReady || p.state == StateStarting)
	p.mu.Unlock()
	if unexpected {
		select {
		case p.crashCh <- "child exited unexpectedly":
		default:
		}
	}
}

func (p *Proxy) routeFromChild(raw []byte) {
	msg, perr := Parse(raw)
	if perr != nil {
		_ = p.writeClient(raw)
		return
	}

	if msg.IsResponse() {
		key := msg.IDKey()
		p.mu.Lock()
		if p.swallow[key] {
			delete(p.swallow, key)
			done := p.handshakeDone
			p.mu.Unlock()
			p.recordProtocolVersion(raw) // compare/warn against first handshake
			if done != nil {
				select {
				case done <- struct{}{}:
				default:
				}
			}
			return // sentinel response: never forwarded to the client
		}
		_, tracked := p.inFlight[key]
		if tracked {
			delete(p.inFlight, key)
			emptiedWhileDraining := len(p.inFlight) == 0 && p.state == StateDraining
			p.mu.Unlock()
			_ = p.writeClient(raw)
			if emptiedWhileDraining {
				select {
				case p.idleCh <- struct{}{}:
				default:
				}
			}
			return
		}
		p.mu.Unlock()
		// Untracked response: e.g. the client's own initialize response, or a
		// reply to a server-initiated request. Forward as-is, and opportunistically
		// learn the protocol version from the first initialize response.
		p.recordProtocolVersion(raw)
		_ = p.writeClient(raw)
		return
	}

	// Server-initiated request or notification destined for the client.
	_ = p.writeClient(raw)
}

func (p *Proxy) recordProtocolVersion(raw []byte) {
	pv := extractProtocolVersion(raw)
	if pv == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.protoVersion == "" {
		p.protoVersion = pv
		return
	}
	if pv != p.protoVersion {
		p.logf("warning: protocolVersion changed across restart: %q -> %q", p.protoVersion, pv)
	}
}

// ---- control / restart orchestration --------------------------------------

func (p *Proxy) controlLoop(ctx context.Context) {
	var tick <-chan time.Time
	if p.opts.CheckInterval > 0 {
		t := time.NewTicker(p.opts.CheckInterval)
		defer t.Stop()
		tick = t.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case reason := <-p.crashCh:
			p.maybeRestart(ctx, reason, true)
		case <-tick:
			should, reason := false, ""
			if p.opts.RestartReason != nil {
				should, reason = p.opts.RestartReason()
			}
			if should {
				p.maybeRestart(ctx, reason, false)
			}
		}
	}
}

// maybeRestart performs the full drain/kill/spawn/re-handshake/flush cycle. It
// is a no-op unless the proxy is in a restartable state. For crashes the captured
// handshake (if any) is replayed against the new child just like a planned restart.
func (p *Proxy) maybeRestart(ctx context.Context, reason string, crash bool) {
	p.mu.Lock()
	if p.state != StateReady && p.state != StateStarting {
		p.mu.Unlock()
		return // a restart is already in progress
	}
	if !p.handshakeCaptured && !crash {
		// Guard: never restart on a schedule before we have a handshake to
		// replay (prevents wedging the child on a too-short interval).
		p.mu.Unlock()
		return
	}
	p.state = StateDraining
	p.mu.Unlock()
	p.logf("state -> Draining (reason: %s)", reason)

	forced := p.drain(ctx)
	if ctx.Err() != nil {
		return
	}

	p.mu.Lock()
	p.state = StateRestarting
	var abandoned [][]byte
	if forced {
		for _, id := range p.inFlight {
			abandoned = append(abandoned, errorResponse(id, retryableErrorCode, "mcp-launcher: child restarting, please retry"))
		}
		p.inFlight = make(map[string]json.RawMessage)
	}
	oldChild := p.child
	p.mu.Unlock()
	p.logf("state -> Restarting (forced=%v)", forced)

	for _, e := range abandoned {
		_ = p.writeClient(e)
	}
	if oldChild != nil {
		_ = oldChild.Kill()
	}

	// Spawn + re-handshake, retrying with backoff on failure.
	for {
		if ctx.Err() != nil {
			return
		}
		if p.opts.Refresh != nil {
			if err := p.opts.Refresh(ctx); err != nil {
				p.logf("warning: token refresh failed: %v (continuing)", err)
			}
		}
		child, err := p.opts.Spawn(ctx)
		if err != nil {
			p.logf("warning: spawn failed: %v (backing off)", err)
			if !sleepCtx(ctx, spawnBackoff) {
				return
			}
			continue
		}
		p.mu.Lock()
		p.child = child
		p.mu.Unlock()
		go p.childReader(child)

		if err := p.replayHandshake(ctx, child); err != nil {
			p.logf("warning: re-handshake failed: %v (restarting child)", err)
			_ = child.Kill()
			if !sleepCtx(ctx, spawnBackoff) {
				return
			}
			continue
		}
		break
	}

	p.flushAndReady()
	p.logf("state -> Ready (restart complete)")
}

// drain waits until in-flight reaches zero, or until the drain timeout expires.
// It returns true when the timeout forced the restart. New requests are queued
// (not forwarded) while draining, so in-flight decreases monotonically to zero.
func (p *Proxy) drain(ctx context.Context) bool {
	var deadline <-chan time.Time
	if p.opts.DrainTimeout > 0 {
		t := time.NewTimer(p.opts.DrainTimeout)
		defer t.Stop()
		deadline = t.C
	}
	for {
		p.mu.Lock()
		n := len(p.inFlight)
		p.mu.Unlock()
		if n == 0 {
			return false
		}
		select {
		case <-p.idleCh:
			// re-check under lock on next iteration
		case <-deadline:
			return true
		case <-ctx.Done():
			return false
		}
	}
}

// replayHandshake sends the cached initialize (rewritten to the sentinel id) to
// the new child, swallows its response, then replays notifications/initialized.
func (p *Proxy) replayHandshake(ctx context.Context, child Child) error {
	p.mu.Lock()
	initReq := p.initReq
	initNote := p.initNote
	if initReq == nil {
		p.mu.Unlock()
		return errors.New("no captured initialize to replay")
	}
	done := make(chan struct{}, 1)
	p.handshakeDone = done
	p.swallow[sentinelID] = true
	p.mu.Unlock()

	sentinelInit, err := setID(initReq, json.RawMessage(sentinelID))
	if err != nil {
		return err
	}
	if err := p.writeChild(child, sentinelInit); err != nil {
		return err
	}

	select {
	case <-done:
	case <-time.After(handshakeTimeout):
		p.mu.Lock()
		delete(p.swallow, sentinelID)
		p.mu.Unlock()
		return errors.New("timed out waiting for initialize response")
	case <-ctx.Done():
		return ctx.Err()
	}

	if initNote != nil {
		if err := p.writeChild(child, initNote); err != nil {
			return err
		}
	}
	return nil
}

// flushAndReady writes any queued frames to the new child and transitions back
// to Ready. It drains in batches so frames that arrive during the flush keep
// their relative order, flipping to Ready only once the queue is fully empty.
func (p *Proxy) flushAndReady() {
	for {
		p.mu.Lock()
		if len(p.queue) == 0 {
			p.state = StateReady
			p.mu.Unlock()
			return
		}
		batch := p.queue
		p.queue = nil
		child := p.child
		p.mu.Unlock()

		for _, raw := range batch {
			if msg, err := Parse(raw); err == nil && msg.IsRequest() {
				p.mu.Lock()
				p.inFlight[msg.IDKey()] = msg.IDRaw()
				p.mu.Unlock()
			}
			if child != nil {
				if err := p.writeChild(child, raw); err != nil {
					p.logf("warning: flush write failed: %v", err)
				}
			}
		}
	}
}

// ---- io helpers ------------------------------------------------------------

func (p *Proxy) writeChild(child Child, raw []byte) error {
	p.childWriteMu.Lock()
	defer p.childWriteMu.Unlock()
	_, err := child.Stdin().Write(ensureNewline(raw))
	return err
}

func (p *Proxy) writeClient(raw []byte) error {
	p.clientWriteMu.Lock()
	defer p.clientWriteMu.Unlock()
	_, err := p.opts.ClientOut.Write(ensureNewline(raw))
	return err
}

// readFrame reads one newline-delimited frame. It uses bufio.Reader.ReadBytes
// (not bufio.Scanner) so large tools/list and tools/call payloads above the
// 64KiB Scanner token limit are not truncated. Blank frames are skipped.
func readFrame(r *bufio.Reader) ([]byte, error) {
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if isBlank(line) {
				if err != nil {
					return nil, err
				}
				continue
			}
			return line, err
		}
		return nil, err
	}
}

func isBlank(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			return false
		}
	}
	return true
}

func ensureNewline(raw []byte) []byte {
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		return raw
	}
	out := make([]byte, len(raw)+1)
	copy(out, raw)
	out[len(raw)] = '\n'
	return out
}

func clone(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
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
