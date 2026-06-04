package codexruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// Config describes how to spawn a Codex app-server process.
type Config struct {
	Command string
	Args    []string
	Cwd     string
	Env     []string
	Stderr  io.Writer
}

// Client is a newline-delimited JSON-RPC client for `codex app-server --stdio`.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr io.Reader

	mu       sync.Mutex
	nextID   int
	pending  map[int]chan response
	closed   bool
	failOnce sync.Once

	notifications  chan Message
	serverRequests chan Message
	errors         chan error
	done           chan struct{}
}

// Message is the app-server JSON-RPC wire envelope.
type Message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

// RPCError is the error payload used by app-server responses.
type RPCError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Data) > 0 {
		return fmt.Sprintf("%s: %s", e.Message, string(e.Data))
	}
	return e.Message
}

type response struct {
	msg Message
	err error
}

// Start launches `codex app-server --stdio` and starts the reader loop.
func Start(ctx context.Context, cfg Config) (*Client, error) {
	command := cfg.Command
	if command == "" {
		command = "codex"
	}
	args := cfg.Args
	if len(args) == 0 {
		args = []string{"app-server", "--stdio"}
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = cfg.Cwd
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c := &Client{
		cmd:            cmd,
		stdin:          stdin,
		stderr:         stderr,
		nextID:         1,
		pending:        make(map[int]chan response),
		notifications:  make(chan Message, 256),
		serverRequests: make(chan Message, 64),
		errors:         make(chan error, 8),
		done:           make(chan struct{}),
	}

	go c.read(stdout)
	go c.drainStderr(cfg.Stderr)
	return c, nil
}

// Request sends a request and waits for the response with the matching id.
func (c *Client) Request(ctx context.Context, method string, params any) (Message, error) {
	id, ch, err := c.reserve(method)
	if err != nil {
		return Message{}, err
	}

	if err := c.write(map[string]any{
		"id":     id,
		"method": method,
		"params": params,
	}); err != nil {
		c.release(id)
		return Message{}, err
	}

	select {
	case res := <-ch:
		if res.err != nil {
			return Message{}, res.err
		}
		if res.msg.Error != nil {
			return res.msg, res.msg.Error
		}
		return res.msg, nil
	case <-ctx.Done():
		c.release(id)
		return Message{}, ctx.Err()
	}
}

// Notify sends a notification that does not expect a response.
func (c *Client) Notify(method string, params any) error {
	msg := map[string]any{"method": method}
	if params != nil {
		msg["params"] = params
	}
	return c.write(msg)
}

// Respond sends a successful response to a server-to-client request.
func (c *Client) Respond(request Message, result any) error {
	if len(request.ID) == 0 {
		return errors.New("server request id is required")
	}
	return c.write(map[string]any{
		"id":     request.ID,
		"result": result,
	})
}

// RespondError sends an error response to a server-to-client request.
func (c *Client) RespondError(request Message, code int64, message string, data any) error {
	if len(request.ID) == 0 {
		return errors.New("server request id is required")
	}
	errPayload := map[string]any{
		"code":    code,
		"message": message,
	}
	if data != nil {
		errPayload["data"] = data
	}
	return c.write(map[string]any{
		"id":    request.ID,
		"error": errPayload,
	})
}

// Notifications returns server notifications that do not require a response.
func (c *Client) Notifications() <-chan Message {
	return c.notifications
}

// ServerRequests returns requests initiated by app-server that require a client response.
func (c *Client) ServerRequests() <-chan Message {
	return c.serverRequests
}

// Errors returns asynchronous reader and process errors.
func (c *Client) Errors() <-chan error {
	return c.errors
}

// Close shuts down the app-server process.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		if err := c.cmd.Process.Signal(os.Interrupt); err != nil {
			_ = c.cmd.Process.Kill()
		}
	}

	wait := make(chan error, 1)
	go func() {
		wait <- c.cmd.Wait()
	}()

	select {
	case err := <-wait:
		_ = err
		return nil
	case <-time.After(2 * time.Second):
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		_ = <-wait
		return nil
	}
}

func (c *Client) reserve(method string) (int, chan response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return 0, nil, errors.New("codexruntime client is closed")
	}

	id := c.nextID
	c.nextID++
	ch := make(chan response, 1)
	c.pending[id] = ch
	_ = method
	return id, ch, nil
}

func (c *Client) release(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("codexruntime client is closed")
	}
	_, err = c.stdin.Write(data)
	return err
}

func (c *Client) read(r io.Reader) {
	defer close(c.done)
	defer close(c.notifications)
	defer close(c.serverRequests)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			c.fail(err)
			return
		}
		if len(msg.ID) > 0 {
			if msg.Method != "" {
				c.serverRequests <- msg
				continue
			}
			c.dispatchResponse(msg)
			continue
		}
		c.notifications <- msg
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, os.ErrClosed) {
		c.fail(err)
	}
}

func (c *Client) dispatchResponse(msg Message) {
	id, err := ParseID(msg.ID)
	if err != nil {
		c.report(err)
		return
	}

	c.mu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()

	if !ok {
		c.report(fmt.Errorf("received response for unknown request id %d", id))
		return
	}
	ch <- response{msg: msg}
	close(ch)
}

func (c *Client) fail(err error) {
	c.failOnce.Do(func() {
		c.mu.Lock()
		for id, ch := range c.pending {
			ch <- response{err: err}
			close(ch)
			delete(c.pending, id)
		}
		c.mu.Unlock()
		c.report(err)
	})
}

func (c *Client) report(err error) {
	select {
	case c.errors <- err:
	default:
	}
}

func (c *Client) drainStderr(dst io.Writer) {
	if dst == nil {
		_, _ = io.Copy(io.Discard, c.stderr)
		return
	}
	_, _ = io.Copy(dst, c.stderr)
}

// ParseID parses a JSON-RPC id encoded as a number or numeric string.
func ParseID(raw json.RawMessage) (int, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// ExtractString returns a nested string from a JSON object response.
func ExtractString(raw json.RawMessage, path ...string) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}

	current := value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return "", fmt.Errorf("missing object at %q", key)
		}
		current, ok = object[key]
		if !ok {
			return "", fmt.Errorf("missing field %q", key)
		}
	}

	text, ok := current.(string)
	if !ok {
		return "", fmt.Errorf("field is not a string")
	}
	return text, nil
}
