// Copyright 2026 ICAP Mock

package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/icap-mock/icap-mock/pkg/pool"
)

// ConnectionState represents the current state of a connection.
type ConnectionState int

const (
	// ConnStateActive indicates the connection is actively handling requests.
	ConnStateActive ConnectionState = iota
	// ConnStateIdle indicates the connection is idle and waiting for requests.
	ConnStateIdle
	// ConnStateClosed indicates the connection has been closed.
	ConnStateClosed
)

// String returns a string representation of the connection state.
func (s ConnectionState) String() string {
	switch s {
	case ConnStateActive:
		return "active"
	case ConnStateIdle:
		return "idle"
	case ConnStateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// pooledBuffer wraps a pooled byte slice for buffered I/O operations.
// It implements io.Reader and io.Writer interfaces with buffering support.
// The buffer is returned to the pool when close is called.
// This replaces bufio.Reader/Writer to allow buffer pooling.
type pooledBuffer struct {
	rw     io.ReadWriter
	err    error
	bufPtr *[]byte
	pool   *pool.SlicePool
	buf    []byte
	rpos   int
	wpos   int
}

// newPooledReadBuffer creates a new pooled buffer for reading.
func newPooledReadBuffer(rw io.ReadWriter, p *pool.SlicePool) *pooledBuffer {
	bufPtr := p.Get(pool.SizeMedium)
	// Expand slice to full capacity so Read can fill the buffer.
	// Pool returns slices with len=0, cap=8192; we need len=cap for Read.
	buf := (*bufPtr)[:cap(*bufPtr)]
	return &pooledBuffer{
		buf:    buf,
		bufPtr: bufPtr,
		rw:     rw,
		pool:   p,
	}
}

// Read reads data into p, using the internal buffer for efficiency.
// It implements the io.Reader interface.
func (pb *pooledBuffer) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	// Return any saved error from previous read
	if pb.err != nil {
		err := pb.err
		pb.err = nil
		return 0, err
	}

	if pb.rpos >= pb.wpos {
		// Buffer is empty, refill from underlying reader
		n, err := pb.rw.Read(pb.buf)
		if n > 0 {
			pb.wpos = n
			pb.rpos = 0
		}
		if err != nil {
			// Save error for next call if we have data to return
			if n > 0 {
				pb.err = err
			} else {
				return 0, err
			}
		} else if n == 0 {
			// n == 0 and err == nil shouldn't happen per io.Reader contract
			// But if it does, return EOF
			return 0, io.EOF
		}
	}

	// Copy from buffer to p
	n := copy(p, pb.buf[pb.rpos:pb.wpos])
	pb.rpos += n
	return n, nil
}

// Write writes data to the underlying connection.
// It implements the io.Writer interface.
func (pb *pooledBuffer) Write(p []byte) (int, error) {
	return pb.rw.Write(p)
}

// Flush flushes any buffered write data.
func (pb *pooledBuffer) Flush() error {
	// No buffered writes in current implementation
	return nil
}

// ReadByte reads a single byte from the buffer.
func (pb *pooledBuffer) ReadByte() (byte, error) {
	// Return any saved error from previous read
	if pb.err != nil {
		err := pb.err
		pb.err = nil
		return 0, err
	}

	if pb.rpos >= pb.wpos {
		// Refill buffer
		n, err := pb.rw.Read(pb.buf)
		if n > 0 {
			pb.wpos = n
			pb.rpos = 0
		}
		if err != nil {
			if n == 0 {
				return 0, err
			}
			// Save error for next call
			pb.err = err
		} else if n == 0 {
			return 0, io.EOF
		}
	}
	b := pb.buf[pb.rpos]
	pb.rpos++
	return b, nil
}

// Buffered returns the number of bytes that can be read from the buffer.
func (pb *pooledBuffer) Buffered() int {
	return pb.wpos - pb.rpos
}

// ReadLine reads until newline, returning the line without the newline.
// It scans the buffered data for '\n' to avoid per-byte append allocations.
func (pb *pooledBuffer) ReadLine() ([]byte, error) {
	var line []byte
	for {
		// Ensure we have data in the buffer
		if pb.rpos >= pb.wpos {
			if pb.err != nil {
				err := pb.err
				pb.err = nil
				return line, err
			}
			n, err := pb.rw.Read(pb.buf)
			if n > 0 {
				pb.wpos = n
				pb.rpos = 0
			}
			if err != nil {
				if n == 0 {
					return line, err
				}
				pb.err = err
			} else if n == 0 {
				return line, io.EOF
			}
		}

		// Scan buffered data for newline
		buffered := pb.buf[pb.rpos:pb.wpos]
		idx := bytes.IndexByte(buffered, '\n')
		if idx >= 0 {
			// Found newline — grab everything before it
			segment := buffered[:idx]
			pb.rpos += idx + 1 // skip past '\n'
			if line == nil {
				// Fast path: entire line was in the buffer, return a copy
				result := make([]byte, len(segment))
				copy(result, segment)
				// Remove trailing \r
				if len(result) > 0 && result[len(result)-1] == '\r' {
					result = result[:len(result)-1]
				}
				return result, nil
			}
			line = append(line, segment...)
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			return line, nil
		}

		// No newline found — append all buffered data and refill
		line = append(line, buffered...)
		pb.rpos = pb.wpos // consumed all buffered data
	}
}

// ReadBytes reads until delimiter, returning the bytes including the delimiter.
// It scans the buffered data for the delimiter to avoid per-byte append allocations.
func (pb *pooledBuffer) ReadBytes(delim byte) ([]byte, error) {
	var result []byte
	for {
		// Ensure we have data in the buffer
		if pb.rpos >= pb.wpos {
			if pb.err != nil {
				err := pb.err
				pb.err = nil
				return result, err
			}
			n, err := pb.rw.Read(pb.buf)
			if n > 0 {
				pb.wpos = n
				pb.rpos = 0
			}
			if err != nil {
				if n == 0 {
					return result, err
				}
				pb.err = err
			} else if n == 0 {
				return result, io.EOF
			}
		}

		// Scan buffered data for delimiter
		buffered := pb.buf[pb.rpos:pb.wpos]
		idx := bytes.IndexByte(buffered, delim)
		if idx >= 0 {
			// Found delimiter — grab everything up to and including it
			segment := buffered[:idx+1]
			pb.rpos += idx + 1
			if result == nil {
				// Fast path: entire result was in the buffer
				out := make([]byte, len(segment))
				copy(out, segment)
				return out, nil
			}
			result = append(result, segment...)
			return result, nil
		}

		// No delimiter found — append all buffered data and refill
		result = append(result, buffered...)
		pb.rpos = pb.wpos
	}
}

// ReadString reads until delimiter, returning a string including the delimiter.
func (pb *pooledBuffer) ReadString(delim byte) (string, error) {
	b, err := pb.ReadBytes(delim)
	return string(b), err
}

// Reset resets the buffer for reuse.
func (pb *pooledBuffer) Reset(rw io.ReadWriter) {
	pb.rpos = 0
	pb.wpos = 0
	pb.rw = rw
}

// close returns the buffer to the pool.
func (pb *pooledBuffer) close() {
	if pb.bufPtr != nil && pb.pool != nil {
		pb.pool.Put(pb.bufPtr)
		pb.bufPtr = nil
		pb.buf = nil
	}
}

// bufferedWriter provides buffered writing using a pooled buffer.
// It implements io.Writer, io.StringWriter, and Flusher interfaces.
//
// Buffering Strategy:
//   - Small writes are accumulated in the internal 8KB buffer
//   - When the buffer is full, it's flushed to the underlying writer
//   - Large writes (>8KB) bypass the buffer and go directly to reduce memory copies
//   - Call Flush() before response completion to ensure all data is sent
//
// This reduces syscall overhead by coalescing multiple small writes into
// fewer larger writes. For typical ICAP responses with many small header
// lines, this can reduce syscalls by 10-20x.
type bufferedWriter struct {
	w      io.Writer
	bufPtr *[]byte
	pool   *pool.SlicePool
	buf    []byte
}

// newBufferedWriter creates a new buffered writer with a pooled 8KB buffer.
// The buffer is obtained from the pool and will be returned when close() is called.
func newBufferedWriter(w io.Writer, p *pool.SlicePool) *bufferedWriter {
	bufPtr := p.Get(pool.SizeMedium)
	return &bufferedWriter{
		w:      w,
		buf:    (*bufPtr)[:0], // Start with empty buffer (len=0, cap=8192)
		bufPtr: bufPtr,
		pool:   p,
	}
}

// Write writes data to the buffer, flushing when full.
// For large writes that exceed available buffer space, it flushes the buffer
// first and then writes directly to reduce memory copies.
// This reduces syscall overhead by coalescing small writes.
func (bw *bufferedWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	available := cap(bw.buf) - len(bw.buf)

	// If the write is larger than available buffer space, handle specially
	if len(p) > available {
		// For writes larger than the entire buffer capacity, bypass entirely
		if len(p) >= cap(bw.buf) {
			// Flush any existing buffered data first
			if err := bw.Flush(); err != nil {
				return 0, err
			}
			// Write directly to avoid extra copy
			return bw.w.Write(p)
		}

		// Write fits in buffer after flush - flush and then buffer
		if err := bw.Flush(); err != nil {
			return 0, err
		}
	}

	// Append to buffer
	bw.buf = append(bw.buf, p...)
	return len(p), nil
}

// WriteString writes a string to the buffer.
// It implements the io.StringWriter interface.
// This is more efficient than converting to []byte first.
func (bw *bufferedWriter) WriteString(s string) (int, error) {
	if s == "" {
		return 0, nil
	}

	available := cap(bw.buf) - len(bw.buf)

	// If the write is larger than available buffer space, handle specially
	if len(s) > available {
		// For writes larger than the entire buffer capacity, bypass entirely
		if len(s) >= cap(bw.buf) {
			// Flush any existing buffered data first
			if err := bw.Flush(); err != nil {
				return 0, err
			}
			// Write directly to avoid extra copy
			return io.WriteString(bw.w, s)
		}

		// Write fits in buffer after flush - flush and then buffer
		if err := bw.Flush(); err != nil {
			return 0, err
		}
	}

	// Append to buffer
	bw.buf = append(bw.buf, s...)
	return len(s), nil
}

// Flush writes any buffered data to the underlying writer.
// It should be called before response completion to ensure all data is sent.
// After Flush, the buffer is reset and ready for new writes.
func (bw *bufferedWriter) Flush() error {
	if len(bw.buf) == 0 {
		return nil
	}

	_, err := bw.w.Write(bw.buf)
	// Reset buffer for reuse (keep capacity, set length to 0)
	bw.buf = bw.buf[:0]
	return err
}

// close flushes any remaining data and returns the buffer to the pool.
// After calling close, the bufferedWriter should not be used.
func (bw *bufferedWriter) close() {
	// Flush any remaining buffered data
	_ = bw.Flush() // Ignore error - connection is closing anyway

	// Return buffer to pool
	if bw.bufPtr != nil && bw.pool != nil {
		bw.pool.Put(bw.bufPtr)
		bw.bufPtr = nil
		bw.buf = nil
	}
}

// ConnectionConfig holds configuration for a single connection.
type ConnectionConfig struct {
	// ReadTimeout is the maximum duration for reading the request.
	ReadTimeout time.Duration
	// WriteTimeout is the maximum duration for writing the response.
	WriteTimeout time.Duration
	// MaxBodySize is the maximum size of the request body.
	MaxBodySize int64
	// Streaming enables full streaming mode for body handling.
	Streaming bool
	// IdleTimeout is the maximum duration a connection can be idle.
	IdleTimeout time.Duration
}

// Connection represents a single client connection to the ICAP server.
// It wraps a net.Conn and provides additional functionality like
// timeouts, state management, and pooled buffered I/O.
type Connection struct {
	// conn is the underlying network connection.
	conn net.Conn
	// config holds the connection configuration.
	config *ConnectionConfig
	// reader is a pooled buffered reader for the connection.
	reader *pooledBuffer
	// writer is a pooled buffered writer for the connection.
	writer *bufferedWriter
	// remoteAddr is the string representation of the remote address.
	remoteAddr string
	// state is the current connection state.
	state ConnectionState
	// stateMu protects access to state.
	stateMu sync.RWMutex
	// closed tracks whether the connection has been closed.
	closed bool
	// closedMu protects access to closed.
	closedMu sync.Mutex
	// once ensures Close is only called once.
	once sync.Once
	// lastActivityNano stores the last activity timestamp as UnixNano using atomic operations.
	// Used for idle timeout detection without mutex overhead.
	lastActivityNano atomic.Int64
}

// newConnection creates a new Connection from a net.Conn.
// It initializes the pooled buffered reader/writer to reduce GC pressure.
func newConnection(conn net.Conn, config *ConnectionConfig) *Connection {
	c := &Connection{
		conn:       conn,
		config:     config,
		remoteAddr: conn.RemoteAddr().String(),
		state:      ConnStateActive,
	}
	c.lastActivityNano.Store(time.Now().UnixNano())

	// Use pooled buffers for buffered I/O
	// Create pooled read buffer with pooled memory
	c.reader = newPooledReadBuffer(conn, pool.BufferPool)

	// Create pooled buffered writer
	c.writer = newBufferedWriter(conn, pool.BufferPool)

	return c
}

// UpdateActivity updates the last activity timestamp to the current time.
// This should be called after successful read or write operations.
// Uses atomic operations instead of mutex for minimal overhead on hot path.
func (c *Connection) UpdateActivity() {
	c.lastActivityNano.Store(time.Now().UnixNano())
}

// LastActivity returns the timestamp of the last activity on this connection.
// Uses atomic operations instead of mutex for lock-free reads.
func (c *Connection) LastActivity() time.Time {
	return time.Unix(0, c.lastActivityNano.Load())
}

// IsIdle checks if the connection has been idle for longer than the configured timeout.
// Returns true if the connection has been idle, false otherwise.
// If IdleTimeout is 0 or negative, this always returns false (no idle timeout).
func (c *Connection) IsIdle() bool {
	if c.config.IdleTimeout <= 0 {
		return false
	}
	idleTime := time.Since(c.LastActivity())
	return idleTime > c.config.IdleTimeout
}

// Read reads data from the connection with the configured read timeout.
// It implements the io.Reader interface.
func (c *Connection) Read(b []byte) (n int, err error) {
	if c.config.ReadTimeout > 0 {
		if dlErr := c.conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout)); dlErr != nil {
			return 0, dlErr
		}
	}
	n, err = c.reader.Read(b)
	if n > 0 {
		c.UpdateActivity()
	}
	return n, err
}

// Write writes data to the connection with the configured write timeout.
// It implements the io.Writer interface.
func (c *Connection) Write(b []byte) (n int, err error) {
	if c.config.WriteTimeout > 0 {
		if dlErr := c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout)); dlErr != nil {
			return 0, dlErr
		}
	}
	n, err = c.writer.Write(b)
	if n > 0 {
		c.UpdateActivity()
	}
	return n, err
}

// Close closes the connection.
// It is safe to call Close multiple times.
// It flushes any pending data in the write buffer before closing
// and returns pooled buffers to reduce GC pressure.
func (c *Connection) Close() error {
	var err error
	c.once.Do(func() {
		c.SetState(ConnStateClosed)
		c.closedMu.Lock()
		c.closed = true
		c.closedMu.Unlock()

		// Flush any remaining data in the write buffer before closing
		if flushErr := c.writer.Flush(); flushErr != nil {
			// Log flush error but continue with close
			// We still want to close the connection even if flush fails
			err = flushErr
		}

		if closeErr := c.conn.Close(); closeErr != nil {
			// Close error takes precedence if no flush error
			if err == nil {
				err = closeErr
			}
		}

		// Return pooled buffers to reduce GC pressure
		if c.reader != nil {
			c.reader.close()
			c.reader = nil
		}
		if c.writer != nil {
			c.writer.close()
			c.writer = nil
		}
	})
	return err
}

// SetReadDeadline sets the read deadline on the underlying connection.
func (c *Connection) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline on the underlying connection.
func (c *Connection) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// RemoteAddr returns the remote network address as a string.
func (c *Connection) RemoteAddr() string {
	return c.remoteAddr
}

// State returns the current connection state.
func (c *Connection) State() ConnectionState {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state
}

// SetState sets the connection state.
func (c *Connection) SetState(state ConnectionState) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.state = state
}

// Reader returns the pooled buffered reader for the connection.
// Use this for reading ICAP requests.
func (c *Connection) Reader() BufferedReader {
	return c.reader
}

// Writer returns the pooled buffered writer for the connection.
// Use this for writing ICAP responses.
func (c *Connection) Writer() BufferedWriter {
	return c.writer
}

// Flush flushes any buffered data to the underlying connection.
func (c *Connection) Flush() error {
	return c.writer.Flush()
}

// IsClosed returns true if the connection has been closed.
func (c *Connection) IsClosed() bool {
	c.closedMu.Lock()
	defer c.closedMu.Unlock()
	return c.closed
}

// LocalAddr returns the local network address.
func (c *Connection) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// ConnectionPool manages a pool of active connections.
// It provides thread-safe operations for adding, removing, and closing connections.
type ConnectionPool struct {
	// connections maps connection pointers to their presence in the pool.
	connections map[*Connection]struct{}
	// mu protects access to connections.
	mu sync.RWMutex
	// wg tracks active connections for graceful shutdown.
	wg sync.WaitGroup
}

// NewConnectionPool creates a new empty ConnectionPool.
func NewConnectionPool() *ConnectionPool {
	return &ConnectionPool{
		connections: make(map[*Connection]struct{}),
	}
}

// Add adds a connection to the pool.
func (p *ConnectionPool) Add(conn *Connection) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connections[conn] = struct{}{}
	p.wg.Add(1)
}

// Remove removes a connection from the pool.
func (p *ConnectionPool) Remove(conn *Connection) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.connections[conn]; exists {
		delete(p.connections, conn)
		p.wg.Done()
	}
}

// Count returns the number of connections in the pool.
func (p *ConnectionPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.connections)
}

// CloseAll closes all connections in the pool and removes them.
// It waits for all connections to be removed from the pool before returning.
func (p *ConnectionPool) CloseAll(_ context.Context) {
	p.mu.Lock()
	// Get a copy of connections to avoid holding lock during close
	conns := make([]*Connection, 0, len(p.connections))
	for conn := range p.connections {
		conns = append(conns, conn)
	}
	// Clear the pool immediately
	p.connections = make(map[*Connection]struct{})
	p.mu.Unlock()

	// Close all connections
	for _, conn := range conns {
		_ = conn.Close()
		// Mark as done in wait group
		p.wg.Done()
	}
}

// Wait blocks until all connections have been removed from the pool.
// This is useful for graceful shutdown - wait for all requests to complete.
// If the context times out, it logs how many connections remain active.
func (p *ConnectionPool) Wait(ctx context.Context) {
	// Create a channel that closes when WaitGroup is done
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	// Wait for either completion or context cancellation
	select {
	case <-done:
		return
	case <-ctx.Done():
		// Log how many connections are still active on timeout
		p.mu.RLock()
		remaining := len(p.connections)
		p.mu.RUnlock()
		if remaining > 0 {
			logPrintf("[CONNECTION] Wait() timed out with %d connections still active", remaining)
		}
		return
	}
}

// logPrintf provides a simple logging function to avoid importing log/slog.
// This is a minimal implementation for connection pool logging.
func logPrintf(format string, args ...any) {
	// Use a simple print to stdout - the server package has proper logging
	// This avoids circular dependencies and keeps the connection package lightweight
	fmt.Printf(format+"\n", args...)
}

// List returns a snapshot of all connections in the pool.
func (p *ConnectionPool) List() []*Connection {
	p.mu.RLock()
	defer p.mu.RUnlock()

	conns := make([]*Connection, 0, len(p.connections))
	for conn := range p.connections {
		conns = append(conns, conn)
	}
	return conns
}
