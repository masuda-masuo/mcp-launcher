package mcpproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer is an in-process MCP server that reproduces the lifecycle bug: it
// rejects tools/* with "method invalid during initialization" until it has
// received both an initialize request and a notifications/initialized.
type fakeServer struct {
	inR  *io.PipeReader
	inW  *io.PipeWriter
	outR *io.PipeReader
	outW *io.PipeWriter
	done chan struct{}

	big  bool // tools/call returns a >64KiB payload
	hold bool // tools/call is never answered (simulates a long in-flight op)

	mu             sync.Mutex
	gotInitialize  bool
	gotInitialized bool
}

func newFakeServer(big, hold bool) *fakeServer {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	f := &fakeServer{inR: inR, inW: inW, outR: outR, outW: outW, done: make(chan struct{}), big: big, hold: hold}
	go f.loop()
	return f
}

func (f *fakeServer) Stdin() io.Writer  { return f.inW }
func (f *fakeServer) Stdout() io.Reader { return f.outR }

func (f *fakeServer) Kill() error {
	f.inR.Close() // unblock the server read loop
	<-f.done
	f.inW.Close()
	return nil
}

func (f *fakeServer) loop() {
	defer close(f.done)
	defer f.outW.Close()
	r := bufio.NewReader(f.inR)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			f.handle(line)
		}
		if err != nil {
			return
		}
	}
}

func (f *fakeServer) handle(line []byte) {
	var m struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(bytes.TrimRight(line, "\r\n"), &m); err != nil {
		return
	}
	switch m.Method {
	case "initialize":
		f.mu.Lock()
		f.gotInitialize = true
		f.mu.Unlock()
		f.write(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"fake"}}}`, m.ID))
	case "notifications/initialized":
		f.mu.Lock()
		f.gotInitialized = true
		f.mu.Unlock()
	case "tools/call":
		f.mu.Lock()
		ready := f.gotInitialize && f.gotInitialized
		f.mu.Unlock()
		if !ready {
			f.write(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32002,"message":"method invalid during initialization"}}`, m.ID))
			return
		}
		if f.hold {
			return // never answer: stays in-flight
		}
		text := "ok"
		if f.big {
			text = strings.Repeat("x", 100*1024) // 100 KiB, well over the 64KiB Scanner limit
		}
		b, _ := json.Marshal(text)
		f.write(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":%s}]}}`, m.ID, b))
	}
}

func (f *fakeServer) write(s string) { _, _ = f.outW.Write([]byte(s + "\n")) }

func (f *fakeServer) ready() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotInitialize && f.gotInitialized
}

// harness wires a Proxy to in-memory client pipes and captures client output.
type harness struct {
	clientInW  *io.PipeWriter
	clientOutR *io.PipeReader

	mu      sync.Mutex
	frames  []Message
	servers []*fakeServer

	proxy   *Proxy
	restart int32 // set to 1 to make RestartReason fire once
}

func newHarness(t *testing.T, ctx context.Context, big, hold bool, drain time.Duration) *harness {
	return newHarnessWithRefresh(t, ctx, big, hold, drain, nil)
}

func newHarnessWithRefresh(t *testing.T, ctx context.Context, big, hold bool, drain time.Duration, refresh func(context.Context) error) *harness {
	t.Helper()
	cInR, cInW := io.Pipe()
	cOutR, cOutW := io.Pipe()
	h := &harness{clientInW: cInW, clientOutR: cOutR}

	p := New(Options{
		ClientIn:  cInR,
		ClientOut: cOutW,
		Spawn: func(context.Context) (Child, error) {
			s := newFakeServer(big, hold)
			h.mu.Lock()
			h.servers = append(h.servers, s)
			h.mu.Unlock()
			return s, nil
		},
		Refresh: refresh,
		RestartReason: func() (bool, string) {
			if atomic.CompareAndSwapInt32(&h.restart, 1, 0) {
				return true, "test-triggered"
			}
			return false, ""
		},
		CheckInterval: 5 * time.Millisecond,
		DrainTimeout:  drain,
		Logf:          func(f string, a ...any) { t.Logf("[proxy] "+f, a...) },
	})

	// Collect every frame the proxy emits to the client.
	go func() {
		r := bufio.NewReader(cOutR)
		for {
			line, err := readFrame(r)
			if line != nil {
				msg, _ := Parse(line)
				h.mu.Lock()
				h.frames = append(h.frames, msg)
				h.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	go func() { _ = p.Run(ctx) }()
	h.proxy = p
	return h
}

func (h *harness) send(s string) { _, _ = h.clientInW.Write([]byte(s + "\n")) }

func (h *harness) waitFrame(t *testing.T, pred func(Message) bool, timeout time.Duration) Message {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		for _, m := range h.frames {
			if pred(m) {
				h.mu.Unlock()
				return m
			}
		}
		h.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for expected frame")
	return Message{}
}

func (h *harness) eventually(t *testing.T, cond func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v: %s", timeout, msg)
}

func (h *harness) serverCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.servers)
}

func (h *harness) server(i int) *fakeServer {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.servers[i]
}

func respWithID(key string) func(Message) bool {
	return func(m Message) bool { return m.IsResponse() && m.IDKey() == key }
}

func isErrorResp(raw []byte) bool {
	var r struct {
		Error *json.RawMessage `json:"error"`
	}
	_ = json.Unmarshal(bytes.TrimRight(raw, "\r\n"), &r)
	return r.Error != nil
}

func errorCode(raw []byte) int {
	var r struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(bytes.TrimRight(raw, "\r\n"), &r)
	return r.Error.Code
}

func resultText(raw []byte) string {
	var r struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	_ = json.Unmarshal(bytes.TrimRight(raw, "\r\n"), &r)
	if len(r.Result.Content) == 0 {
		return ""
	}
	return r.Result.Content[0].Text
}

// handshake drives the initial MCP handshake and waits for child #0 to be ready.
func (h *harness) handshake(t *testing.T) {
	t.Helper()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)
	h.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	h.waitFrame(t, respWithID("1"), 2*time.Second) // initialize response reaches client
	h.eventually(t, func() bool { return h.serverCount() >= 1 && h.server(0).ready() }, 2*time.Second, "child 0 initialized")
}

func (h *harness) triggerRestartAndWait(t *testing.T) {
	t.Helper()
	before := h.serverCount()
	atomic.StoreInt32(&h.restart, 1)
	h.eventually(t, func() bool {
		return h.serverCount() > before && h.server(h.serverCount()-1).ready() && h.proxy.State() == StateReady
	}, 3*time.Second, "restart to complete and new child to be re-initialized")
}

func TestReHandshakeAfterRestart_ToolsCallSucceeds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newHarness(t, ctx, false, false, time.Second)

	h.handshake(t)

	h.send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"x"}}`)
	r2 := h.waitFrame(t, respWithID("2"), 2*time.Second)
	if isErrorResp(r2.Raw) {
		t.Fatalf("tools/call before restart should succeed, got error: %s", r2.Raw)
	}

	h.triggerRestartAndWait(t)

	// The new child must have received the replayed handshake.
	if !h.server(1).gotInitialize || !h.server(1).gotInitialized {
		t.Fatalf("new child did not receive replayed handshake (init=%v initialized=%v)",
			h.server(1).gotInitialize, h.server(1).gotInitialized)
	}

	h.send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"x"}}`)
	r3 := h.waitFrame(t, respWithID("3"), 2*time.Second)
	if isErrorResp(r3.Raw) {
		t.Fatalf("tools/call after restart should succeed, got error: %s", r3.Raw)
	}
}

func TestClientNeverSeesSentinelInitializeResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newHarness(t, ctx, false, false, time.Second)

	h.handshake(t)
	h.triggerRestartAndWait(t)
	// Give any stray frame a chance to surface.
	time.Sleep(50 * time.Millisecond)

	h.mu.Lock()
	defer h.mu.Unlock()
	initResponses := 0
	for _, m := range h.frames {
		if m.IDKey() == sentinelID {
			t.Fatalf("client received a frame carrying the sentinel id: %s", m.Raw)
		}
		if m.IsResponse() && m.IDKey() == "1" {
			initResponses++
		}
	}
	if initResponses != 1 {
		t.Fatalf("expected exactly 1 initialize response to reach the client, got %d", initResponses)
	}
}

func TestForcedRestartReturnsRetryableErrorToInFlight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// hold=true: the server never answers tools/call, so it stays in-flight and
	// the drain times out, forcing the restart.
	h := newHarness(t, ctx, false, true, 50*time.Millisecond)

	h.handshake(t)

	h.send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"slow"}}`)
	// Wait until the proxy has registered it as in-flight.
	h.eventually(t, func() bool {
		h.proxy.mu.Lock()
		defer h.proxy.mu.Unlock()
		return len(h.proxy.inFlight) == 1
	}, 2*time.Second, "request to be in-flight")

	atomic.StoreInt32(&h.restart, 1)

	r2 := h.waitFrame(t, respWithID("2"), 3*time.Second)
	if !isErrorResp(r2.Raw) {
		t.Fatalf("expected retryable error for abandoned in-flight request, got: %s", r2.Raw)
	}
	if code := errorCode(r2.Raw); code != retryableErrorCode {
		t.Fatalf("expected error code %d, got %d (%s)", retryableErrorCode, code, r2.Raw)
	}
}

func TestLargeMessageNotTruncated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newHarness(t, ctx, true, false, time.Second) // big payloads

	h.handshake(t)

	h.send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"big"}}`)
	r2 := h.waitFrame(t, respWithID("2"), 3*time.Second)
	if got := len(resultText(r2.Raw)); got != 100*1024 {
		t.Fatalf("large result truncated: expected %d bytes, got %d", 100*1024, got)
	}
}

// TestRefreshNotCalledOnInitialSpawn verifies the fix for issue #14:
// Refresh must NOT block the initial spawn path.  A slow Refresh (longer than
// the MCP client's initialize timeout) must not prevent the child from starting.
func TestRefreshNotCalledOnInitialSpawn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var refreshCalls int32
	slowRefresh := func(ctx context.Context) error {
		atomic.AddInt32(&refreshCalls, 1)
		return nil
	}

	h := newHarnessWithRefresh(t, ctx, false, false, time.Second, slowRefresh)

	// The initial spawn must complete without Refresh being called.
	// We verify this by confirming the handshake succeeds immediately and that
	// refreshCalls is still 0 at that point.
	h.handshake(t)

	if n := atomic.LoadInt32(&refreshCalls); n != 0 {
		t.Fatalf("Refresh was called %d time(s) before or during initial spawn; want 0 (issue #14)", n)
	}

	// After a restart Refresh must be called exactly once (before the re-spawn).
	h.triggerRestartAndWait(t)

	if n := atomic.LoadInt32(&refreshCalls); n != 1 {
		t.Fatalf("Refresh was called %d time(s) after one restart; want 1", n)
	}
}
