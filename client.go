package stun

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Dial connects to the address on the named network and then
// initializes Client on that connection, returning error if any.
func Dial(network, address string) (*Client, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	return NewClient(ClientOptions{
		Connection: conn,
	})
}

// ClientOptions are used to initialize Client.
type ClientOptions struct {
	Agent       ClientAgent
	Connection  Connection
	TimeoutRate time.Duration // defaults to 100 ms
}

const defaultTimeoutRate = time.Millisecond * 100

// ErrNoConnection means that ClientOptions.Connection is nil.
var ErrNoConnection = errors.New("no connection provided")

// NewClient initializes new Client from provided options,
// starting internal goroutines and using default options fields
// if necessary. Call Close method after using Client to release
// resources.
func NewClient(options ClientOptions) (*Client, error) {
	c := &Client{
		close:       make(chan struct{}),
		c:           options.Connection,
		a:           options.Agent,
		gcRate:      options.TimeoutRate,
		clock:       systemClock,
		rto:         int64(time.Millisecond * 500),
		maxAttempts: 7,
		t:           make(map[transactionID]clientTransaction, 100),
	}
	if c.c == nil {
		return nil, ErrNoConnection
	}
	if c.a == nil {
		c.a = NewAgent(AgentOptions{})
	}
	if c.gcRate == 0 {
		c.gcRate = defaultTimeoutRate
	}
	c.wg.Add(2)
	go c.readUntilClosed()
	go c.collectUntilClosed()
	runtime.SetFinalizer(c, clientFinalizer)
	return c, nil
}

func clientFinalizer(c *Client) {
	if c == nil {
		return
	}
	err := c.Close()
	if err == ErrClientClosed {
		return
	}
	if err == nil {
		log.Println("client: called finalizer on non-closed client")
		return
	}
	log.Println("client: called finalizer on non-closed client:", err)
}

// Connection wraps Reader, Writer and Closer interfaces.
type Connection interface {
	io.Reader
	io.Writer
	io.Closer
}

// ClientAgent is Agent implementation that is used by Client to
// process transactions.
type ClientAgent interface {
	Process(*Message) error
	Close() error
	Start(id [TransactionIDSize]byte, deadline time.Time, f Handler) error
	Stop(id [TransactionIDSize]byte) error
	Collect(time.Time) error
}

// Client simulates "connection" to STUN server.
type Client struct {
	a           ClientAgent
	c           Connection
	close       chan struct{}
	gcRate      time.Duration
	rto         int64 // time.Duration
	maxAttempts int32
	closed      bool
	closedMux   sync.RWMutex
	wg          sync.WaitGroup
	clock       Clock

	t    map[transactionID]clientTransaction
	tMux sync.RWMutex
}

// clientTransaction represents transaction in progress.
// If transaction is succeed or failed, f will be called
// provided by event.
// Concurrent access is invalid.
type clientTransaction struct {
	id      transactionID
	start   time.Time
	rto     time.Duration
	attempt int32
	h       Handler
	raw     []byte
}

func (t clientTransaction) nextTimeout(now time.Time) time.Time {
	return now.Add(time.Duration(t.attempt) * t.rto)
}

// Start registers transaction with provided id, deadline and callback.
// Could return ErrAgentClosed, ErrTransactionExists.
// Callback f is guaranteed to be eventually called. See AgentFn for
// callback processing constraints.
func (c *Client) start(t clientTransaction) error {
	c.tMux.Lock()
	defer c.tMux.Unlock()
	if c.closed {
		return ErrAgentClosed
	}
	_, exists := c.t[t.id]
	if exists {
		return ErrTransactionExists
	}
	c.t[t.id] = t
	return nil
}

type Clock interface {
	Now() time.Time
}

type systemClockService struct{}

func (systemClockService) Now() time.Time { return time.Now() }

var systemClock = systemClockService{}

// SetRTO sets current RTO value.
func (c *Client) SetRTO(rto time.Duration) {
	atomic.StoreInt64(&c.rto, int64(rto))
}

// StopErr occurs when Client fails to stop transaction while
// processing error.
type StopErr struct {
	Err   error // value returned by Stop()
	Cause error // error that caused Stop() call
}

func (e StopErr) Error() string {
	return fmt.Sprintf("error while stopping due to %s: %s",
		sprintErr(e.Cause), sprintErr(e.Err),
	)
}

// CloseErr indicates client close failure.
type CloseErr struct {
	AgentErr      error
	ConnectionErr error
}

func sprintErr(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func (c CloseErr) Error() string {
	return fmt.Sprintf("failed to close: %s (connection), %s (agent)",
		sprintErr(c.ConnectionErr), sprintErr(c.AgentErr),
	)
}

func (c *Client) readUntilClosed() {
	defer c.wg.Done()
	m := new(Message)
	m.Raw = make([]byte, 1024)
	for {
		select {
		case <-c.close:
			return
		default:
		}
		_, err := m.ReadFrom(c.c)
		if err == nil {
			if pErr := c.a.Process(m); pErr == ErrAgentClosed {
				return
			}
		}
	}
}

func closedOrPanic(err error) {
	if err == nil || err == ErrAgentClosed {
		return
	}
	panic(err)
}

func (c *Client) collectUntilClosed() {
	t := time.NewTicker(c.gcRate)
	defer c.wg.Done()
	for {
		select {
		case <-c.close:
			t.Stop()
			return
		case gcTime := <-t.C:
			closedOrPanic(c.a.Collect(gcTime))
		}
	}
}

// ErrClientClosed indicates that client is closed.
var ErrClientClosed = errors.New("client is closed")

// Close stops internal connection and agent, returning CloseErr on error.
func (c *Client) Close() error {
	if err := c.checkInit(); err != nil {
		return err
	}
	c.closedMux.Lock()
	if c.closed {
		c.closedMux.Unlock()
		return ErrClientClosed
	}
	c.closed = true
	c.closedMux.Unlock()
	agentErr, connErr := c.a.Close(), c.c.Close()
	close(c.close)
	c.wg.Wait()
	if agentErr == nil && connErr == nil {
		return nil
	}
	return CloseErr{
		AgentErr:      agentErr,
		ConnectionErr: connErr,
	}
}

// Indicate sends indication m to server. Shorthand to Start call
// with zero deadline and callback.
func (c *Client) Indicate(m *Message) error {
	return c.Start(m, nil)
}

// callbackWaitHandler blocks on wait() call until callback is called.
type callbackWaitHandler struct {
	callback  func(event Event)
	cond      *sync.Cond
	processed bool
}

func (s *callbackWaitHandler) HandleEvent(e Event) {
	if s.callback == nil {
		panic("s.callback is nil")
	}
	s.callback(e)
	s.cond.L.Lock()
	s.processed = true
	s.cond.Broadcast()
	s.cond.L.Unlock()
}

func (s *callbackWaitHandler) wait() {
	s.cond.L.Lock()
	for !s.processed {
		s.cond.Wait()
	}
	s.cond.L.Unlock()
}

func (s *callbackWaitHandler) setCallback(f func(event Event)) {
	if f == nil {
		panic("f is nil")
	}
	s.callback = f
}

func (s *callbackWaitHandler) reset() {
	s.processed = false
	s.callback = nil
}

var callbackWaitHandlerPool = sync.Pool{
	New: func() interface{} {
		return &callbackWaitHandler{
			cond: sync.NewCond(new(sync.Mutex)),
		}
	},
}

// ErrClientNotInitialized means that client connection or agent is nil.
var ErrClientNotInitialized = errors.New("client not initialized")

func (c *Client) checkInit() error {
	if c == nil || c.c == nil || c.a == nil || c.close == nil {
		return ErrClientNotInitialized
	}
	return nil
}

func (c *Client) nextTimeout(t time.Time, rto time.Duration) time.Time {
	return t.Add(
		time.Duration(atomic.LoadInt64(&c.rto)),
	)
}

// Do is Start wrapper that waits until callback is called. If no callback
// provided, Indicate is called instead.
//
// Do has cpu overhead due to blocking, see BenchmarkClient_Do.
// Use Start method for less overhead.
func (c *Client) Do(m *Message, f func(Event)) error {
	if err := c.checkInit(); err != nil {
		return err
	}
	if f == nil {
		return c.Indicate(m)
	}
	h := callbackWaitHandlerPool.Get().(*callbackWaitHandler)
	h.setCallback(f)
	defer func() {
		h.reset()
		callbackWaitHandlerPool.Put(h)
	}()
	if err := c.Start(m, h.HandleEvent); err != nil {
		return err
	}
	h.wait()
	return nil
}

// StopWithError removes transaction from list and calls transaction callback
// with provided error. Can return ErrTransactionNotExists and ErrAgentClosed.
func (c *Client) stopWithError(id [TransactionIDSize]byte, err error) error {
	c.closedMux.Lock()
	if c.closed {
		c.closedMux.Unlock()
		return ErrClientClosed
	}
	t, exists := c.t[id]
	delete(c.t, id)
	c.closedMux.Unlock()
	if !exists {
		return ErrTransactionNotExists
	}
	t.h(Event{
		Error: err,
	})
	return nil
}

func (c *Client) delete(id transactionID) {
	c.tMux.Lock()
	if c.t != nil {
		delete(c.t, id)
	}
	c.tMux.Unlock()
}

func (c *Client) handleAgentCallback(e Event) {
	c.tMux.Lock()
	if c.t == nil {
		c.tMux.Unlock()
		return
	}
	t, found := c.t[e.TransactionID]
	delete(c.t, t.id)
	c.tMux.Unlock()
	if !found {
		// Ignoring.
		return
	}

	if atomic.LoadInt32(&c.maxAttempts) < t.attempt || e.Error == nil {
		// Transaction completed.
		t.h(e)
		return
	}

	// Doing re-transmission.
	t.attempt++
	if err := c.start(t); err != nil {
		e.Error = err
		t.h(e)
		return
	}

	// Starting transaction in agent.
	now := c.clock.Now()
	d := t.nextTimeout(now)
	if err := c.a.Start(t.id, d, c.handleAgentCallback); err != nil {
		c.delete(t.id)
		e.Error = err
		t.h(e)
		return
	}

	// Writing message to connection again.
	_, err := c.c.Write(t.raw)
	if err != nil {
		c.delete(t.id)
		e.Error = err

		// Stopping transaction instead of waiting until deadline.
		if stopErr := c.a.Stop(t.id); stopErr != nil {
			e.Error = StopErr{
				Err:   stopErr,
				Cause: err,
			}
		}
		t.h(e)
		return
	}

}

// Start starts transaction (if f set) and writes message to server, handler
// is called asynchronously.
func (c *Client) Start(m *Message, h Handler) error {
	if err := c.checkInit(); err != nil {
		return err
	}
	c.closedMux.RLock()
	closed := c.closed
	c.closedMux.RUnlock()
	if closed {
		return ErrClientClosed
	}
	if h != nil {
		// Starting transaction only if h is set. Useful for indications.
		t := clientTransaction{
			id:      m.TransactionID,
			start:   c.clock.Now(),
			h:       h,
			rto:     time.Duration(atomic.LoadInt64(&c.rto)),
			attempt: 0,
			raw:     make([]byte, 1500),
		}
		n := copy(t.raw, m.Raw)
		t.raw = t.raw[:n]
		if err := c.start(t); err != nil {
			return err
		}
		d := t.nextTimeout(t.start)
		if err := c.a.Start(t.id, d, c.handleAgentCallback); err != nil {
			return err
		}
	}
	_, err := m.WriteTo(c.c)
	if err != nil && h != nil {
		c.delete(m.TransactionID)
		// Stopping transaction instead of waiting until deadline.
		if stopErr := c.a.Stop(m.TransactionID); stopErr != nil {
			return StopErr{
				Err:   stopErr,
				Cause: err,
			}
		}
	}
	return err
}
