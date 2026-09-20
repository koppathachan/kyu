package engine_test

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sasidakh/kyu/internal/engine"
	"github.com/sasidakh/kyu/internal/queue"
	"github.com/sasidakh/kyu/internal/spi"
	"github.com/sasidakh/kyu/internal/storage/memory"
	storagev1 "github.com/sasidakh/kyu/kyu/storage/v1"
	kyuv1 "github.com/sasidakh/kyu/kyu/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// These tests drive the engine from outside its package, through the two
// things a deployment actually has: the kyu.v1.Kyu API, and the
// kyu.storage.v1.Storage server it supplies. Nothing here reaches into engine
// state — where a test wants to know what the engine did, it looks at the
// queue, or at storage, or at the calls the backend received.

// testClock is the engine's clock under test. Holds are made to lapse by
// moving it, not by sleeping: the engine owns the clock, so a test that owns
// the engine's clock owns time.
type testClock struct {
	mu sync.Mutex
	at time.Time
}

func newTestClock() *testClock {
	return &testClock{at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func (c *testClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = t
}

// storageFixture is the reference server on a real socket. The engine reaches
// it over gRPC, never in-process, so that a mistake at the wire contract
// cannot pass unnoticed.
type storageFixture struct {
	t      *testing.T
	addr   string
	srv    *grpc.Server
	client *spi.Client

	mu      sync.Mutex
	calls   map[string]int
	scans   map[string]int
	failing bool
}

func startStorage(t *testing.T) *storageFixture {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	f := &storageFixture{
		t:     t,
		addr:  lis.Addr().String(),
		calls: make(map[string]int),
		scans: make(map[string]int),
	}

	// Every call is counted, and the whole server can be made to fail, so
	// that a test can assert what the engine did and did not do against a
	// backend — a retry, in particular, would show up as a second call.
	f.srv = grpc.NewServer(grpc.UnaryInterceptor(
		func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			f.record(shortMethod(info.FullMethod), req)

			f.mu.Lock()
			failing := f.failing
			f.mu.Unlock()
			if failing {
				return nil, status.Error(codes.Unavailable, "this backend is down")
			}
			return handler(ctx, req)
		}))
	storagev1.RegisterStorageServer(f.srv, memory.New())

	go func() { _ = f.srv.Serve(lis) }()

	// A client of the deployment's own storage server, which is how these
	// tests read a dead letter partition and how they plant a queue the
	// engine did not create.
	f.client, err = spi.Dial(f.addr)
	if err != nil {
		t.Fatalf("dial storage: %v", err)
	}

	t.Cleanup(f.stop)
	return f
}

func (f *storageFixture) stop() {
	_ = f.client.Close()
	f.srv.Stop()
}

func (f *storageFixture) record(method string, req any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[method]++
	if scan, ok := req.(*storagev1.ScanRequest); ok {
		f.scans[scan.GetPartition()]++
	}
}

// fail makes every SPI call return UNAVAILABLE, as a backend that has fallen
// over would.
func (f *storageFixture) fail(failing bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failing = failing
}

func (f *storageFixture) callsTo(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

// scansOf reports how many times a partition has been scanned, which is how a
// test sees whether the reaper is sweeping something.
func (f *storageFixture) scansOf(partition string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scans[partition]
}

func (f *storageFixture) resetCounts() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = make(map[string]int)
	f.scans = make(map[string]int)
}

// partitions is every partition the backend holds, by name.
func (f *storageFixture) partitions() map[string][]byte {
	f.t.Helper()
	list, err := f.client.ListPartitions(context.Background())
	if err != nil {
		f.t.Fatalf("ListPartitions: %v", err)
	}
	out := make(map[string][]byte, len(list))
	for _, p := range list {
		out[p.GetName()] = p.GetSettings()
	}
	return out
}

// contents is every item in a partition, whatever its hold, lowest position
// first. The instant is far enough ahead that no hold hides anything.
func (f *storageFixture) contents(partition string) []*storagev1.Item {
	f.t.Helper()
	items, err := f.client.Scan(context.Background(), partition, farFuture, 0, 1000)
	if err != nil {
		f.t.Fatalf("Scan(%q): %v", partition, err)
	}
	return items
}

func (f *storageFixture) ids(partition string) []string {
	f.t.Helper()
	items := f.contents(partition)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.GetId())
	}
	return out
}

// farFuture is an instant past every hold any test sets.
const farFuture = int64(1) << 62

// shortMethod turns "/kyu.storage.v1.Storage/Claim" into "Claim".
func shortMethod(full string) string {
	if i := strings.LastIndex(full, "/"); i >= 0 {
		return full[i+1:]
	}
	return full
}

// recorder collects what the engine reported, so that a test can assert that
// an ordinary outcome was not reported as a fault.
type recorder struct {
	mu      sync.Mutex
	entries []string
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	if rec.Level < slog.LevelError {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, rec.Message)
	return nil
}

func (r *recorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *recorder) WithGroup(string) slog.Handler      { return r }

func (r *recorder) reported() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.entries...)
}

// engineFixture is one engine instance, served over gRPC, with a client
// attached. More than one may share a storageFixture, which is how the
// multi-replica cases are tested.
type engineFixture struct {
	t       *testing.T
	storage *storageFixture
	clock   *testClock
	grpcSrv *grpc.Server
	server  *engine.Server
	conn    *grpc.ClientConn
	client  kyuv1.KyuClient
	stopped bool
}

func testConfig(storageAddr string) engine.Config {
	cfg := engine.DefaultConfig()
	cfg.StorageTarget = storageAddr
	// Short enough that a test does not wait on the reaper, long enough that
	// the pacing is still exercised rather than bypassed.
	cfg.SweepInterval = 5 * time.Millisecond
	cfg.SweepIntervalMax = 20 * time.Millisecond
	cfg.DiscoveryInterval = 20 * time.Millisecond
	// Measured on the engine's clock, so a test expires it by advancing time
	// rather than by waiting.
	cfg.NegativeCacheTTL = 5 * time.Second
	return cfg
}

func startEngine(t *testing.T, storage *storageFixture, opts ...engine.Option) *engineFixture {
	t.Helper()
	return startEngineWith(t, storage, testConfig(storage.addr), opts...)
}

func startEngineWith(t *testing.T, storage *storageFixture, cfg engine.Config, opts ...engine.Option) *engineFixture {
	t.Helper()

	clock := newTestClock()
	opts = append([]engine.Option{
		engine.WithClock(clock.now),
		engine.WithLogger(slog.New(slog.DiscardHandler)),
	}, opts...)

	eng, err := engine.New(cfg, opts...)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatalf("start engine: %v", err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grpcSrv := grpc.NewServer()
	kyuv1.RegisterKyuServer(grpcSrv, eng)
	go func() { _ = grpcSrv.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial engine: %v", err)
	}

	f := &engineFixture{
		t:       t,
		storage: storage,
		clock:   clock,
		grpcSrv: grpcSrv,
		server:  eng,
		conn:    conn,
		client:  kyuv1.NewKyuClient(conn),
	}
	t.Cleanup(f.stop)
	return f
}

func (f *engineFixture) stop() {
	if f.stopped {
		return
	}
	f.stopped = true
	_ = f.conn.Close()
	f.grpcSrv.Stop()
	_ = f.server.Close()
}

// restart replaces an engine with a fresh one against the same storage,
// keeping the clock, which is how "across a restart" is tested. The new engine
// shares no memory with the old one.
func (f *engineFixture) restart(opts ...engine.Option) *engineFixture {
	f.t.Helper()
	f.stop()

	next := startEngine(f.t, f.storage, opts...)
	next.clock.set(f.clock.now())
	return next
}

func (f *engineFixture) createQueue(name string, hold time.Duration, ceiling uint32) {
	f.t.Helper()
	req := &kyuv1.CreateQueueRequest{Queue: name}
	if hold > 0 {
		req.HoldDuration = durationpb.New(hold)
	}
	if ceiling > 0 {
		req.AttemptCeiling = &ceiling
	}
	if _, err := f.client.CreateQueue(context.Background(), req); err != nil {
		f.t.Fatalf("CreateQueue(%q): %v", name, err)
	}
}

func (f *engineFixture) enqueue(name, payload string) *kyuv1.EnqueueResponse {
	f.t.Helper()
	resp, err := f.client.Enqueue(context.Background(), &kyuv1.EnqueueRequest{
		Queue: name, Payload: []byte(payload),
	})
	if err != nil {
		f.t.Fatalf("Enqueue(%q): %v", name, err)
	}
	return resp
}

// dequeue returns the message, or nil when the queue had nothing available.
func (f *engineFixture) dequeue(name string) *kyuv1.Message {
	f.t.Helper()
	resp, err := f.client.Dequeue(context.Background(), &kyuv1.DequeueRequest{Queue: name})
	if err != nil {
		f.t.Fatalf("Dequeue(%q): %v", name, err)
	}
	return resp.GetMessage()
}

func (f *engineFixture) mustDequeue(name string) *kyuv1.Message {
	f.t.Helper()
	msg := f.dequeue(name)
	if msg == nil {
		f.t.Fatalf("Dequeue(%q) returned nothing, want a message", name)
	}
	return msg
}

func (f *engineFixture) ack(msg *kyuv1.Message) error {
	f.t.Helper()
	_, err := f.client.Ack(context.Background(), &kyuv1.AckRequest{LeaseToken: msg.GetLeaseToken()})
	return err
}

func (f *engineFixture) mustAck(msg *kyuv1.Message) {
	f.t.Helper()
	if err := f.ack(msg); err != nil {
		f.t.Fatalf("Ack(%q): %v", msg.GetId(), err)
	}
}

// dlqContents reads a dead letter partition through the SPI. Nothing in
// kyu.v1 consumes a dead letter queue, so a test that wants to see what
// arrived there looks where an operator would.
func (f *engineFixture) dlqContents(name string) []string {
	f.t.Helper()
	return f.storage.ids(queue.DLQName(name))
}

// eventually waits for a condition the reaper brings about. The reaper's
// pacing runs on real time even though the engine's clock does not, so a test
// that waits on a transfer waits on the wall clock.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func requireCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("status %s, want %s (error: %v)", got, want, err)
	}
}

// streamConsumer drives one StreamingDequeue stream.
//
// One goroutine reads the stream from the moment it opens, so that a test
// waiting for nothing to arrive cannot leave a half-finished read behind to
// swallow what arrives next.
type streamConsumer struct {
	t         *testing.T
	stream    grpc.BidiStreamingClient[kyuv1.StreamingDequeueRequest, kyuv1.StreamingDequeueResponse]
	cancel    context.CancelFunc
	responses chan *kyuv1.StreamingDequeueResponse
	ended     chan error
}

func (f *engineFixture) openStream(name string, inFlight uint32) *streamConsumer {
	f.t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := f.client.StreamingDequeue(ctx)
	if err != nil {
		cancel()
		f.t.Fatalf("StreamingDequeue: %v", err)
	}
	if err := stream.Send(&kyuv1.StreamingDequeueRequest{
		Request: &kyuv1.StreamingDequeueRequest_Open{
			Open: &kyuv1.Open{Queue: name, InFlight: inFlight},
		},
	}); err != nil {
		cancel()
		f.t.Fatalf("send open: %v", err)
	}

	c := &streamConsumer{
		t:         f.t,
		stream:    stream,
		cancel:    cancel,
		responses: make(chan *kyuv1.StreamingDequeueResponse, 64),
		ended:     make(chan error, 1),
	}
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				c.ended <- err
				close(c.responses)
				return
			}
			c.responses <- resp
		}
	}()

	f.t.Cleanup(cancel)
	return c
}

// recv returns the next response, or nil if none arrives within the wait or
// the stream has ended.
func (c *streamConsumer) recv(wait time.Duration) *kyuv1.StreamingDequeueResponse {
	c.t.Helper()
	select {
	case resp, open := <-c.responses:
		if !open {
			return nil
		}
		return resp
	case <-time.After(wait):
		return nil
	}
}

// end returns the error the stream ended with.
func (c *streamConsumer) end(wait time.Duration) error {
	c.t.Helper()
	select {
	case err := <-c.ended:
		return err
	case <-time.After(wait):
		c.t.Fatal("the stream did not end")
		return nil
	}
}

func (c *streamConsumer) mustRecvMessage(wait time.Duration) *kyuv1.Message {
	c.t.Helper()
	resp := c.recv(wait)
	if resp == nil {
		c.t.Fatal("nothing arrived on the stream, want a message")
	}
	msg := resp.GetMessage()
	if msg == nil {
		c.t.Fatalf("the stream carried %v, want a message", resp.GetResponse())
	}
	return msg
}

func (c *streamConsumer) ack(msg *kyuv1.Message) {
	c.t.Helper()
	if err := c.stream.Send(&kyuv1.StreamingDequeueRequest{
		Request: &kyuv1.StreamingDequeueRequest_Ack{
			Ack: &kyuv1.Ack{LeaseToken: msg.GetLeaseToken()},
		},
	}); err != nil {
		c.t.Fatalf("send ack: %v", err)
	}
}

// break_ ends the stream the way a consumer that vanished would.
func (c *streamConsumer) break_() { c.cancel() }
