package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"
	"unicode/utf8"
)

const (
	maxMessage  = 1 << 20
	maxPending  = 32
	maxRequests = 4096
)

var errProtocol = errors.New("codex: incompatible or malformed native protocol")

type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (nativeError *rpcError) Error() string { return "codex: native request refused" }

func requestID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || len(raw) > 256 {
		return "", errProtocol
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil || text == "" {
			return "", errProtocol
		}
		return "s:" + text, nil
	}
	value, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || value < 0 {
		return "", errProtocol
	}
	return "n:" + strconv.FormatInt(value, 10), nil
}

func decodeMessage(line []byte) (message, error) {
	var result message
	if len(line) > maxMessage || !utf8.Valid(line) {
		return result, errProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	if err := uniqueJSON(decoder, 0); err != nil {
		return result, errProtocol
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return result, errProtocol
	}
	decoder = json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil {
		return result, errProtocol
	}
	if result.ID != nil {
		if _, err := requestID(result.ID); err != nil {
			return result, err
		}
	}
	if result.Method != "" {
		if len(result.Method) > 128 || result.Result != nil || result.Error != nil || len(result.Params) == 0 || result.Params[0] != '{' {
			return result, errProtocol
		}
	} else if result.ID == nil || result.Params != nil || (result.Result != nil) == (result.Error != nil) {
		return result, errProtocol
	}
	return result, nil
}

func uniqueJSON(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return errProtocol
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errProtocol
			}
			seen[name] = true
			if err := uniqueJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errProtocol
	}
	_, err = decoder.Token()
	return err
}

type client struct {
	ctx       context.Context
	cancel    context.CancelFunc
	input     io.WriteCloser
	output    io.ReadCloser
	incoming  chan message
	writes    chan struct{}
	mu        sync.Mutex
	next      int64
	pending   map[string]chan message
	err       error
	closeOnce sync.Once
}

func newClient(parent context.Context, output io.ReadCloser, input io.WriteCloser) *client {
	ctx, cancel := context.WithCancel(parent)
	result := &client{ctx: ctx, cancel: cancel, input: input, output: output, incoming: make(chan message, 64), writes: make(chan struct{}, 1), pending: make(map[string]chan message)}
	go func() { <-ctx.Done(); result.close() }()
	go result.read()
	return result
}

func (connection *client) close() {
	connection.closeOnce.Do(func() { connection.cancel(); _ = connection.input.Close(); _ = connection.output.Close() })
}

func (connection *client) fail(err error) {
	connection.mu.Lock()
	if connection.err == nil {
		connection.err = err
	}
	connection.mu.Unlock()
	connection.close()
}

func (connection *client) failure() error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.err != nil {
		return connection.err
	}
	return connection.ctx.Err()
}

func (connection *client) read() {
	defer close(connection.incoming)
	reader := bufio.NewReaderSize(connection.output, 32<<10)
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > maxMessage {
			connection.fail(errProtocol)
			return
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			if len(line) > 0 {
				err = errProtocol
			}
			connection.fail(err)
			return
		}
		value, err := decodeMessage(line)
		line = line[:0]
		if err != nil {
			connection.fail(err)
			return
		}
		if value.Method == "" {
			key, _ := requestID(value.ID)
			connection.mu.Lock()
			response := connection.pending[key]
			delete(connection.pending, key)
			connection.mu.Unlock()
			if response == nil {
				connection.fail(errProtocol)
				return
			}
			response <- value
		} else {
			select {
			case connection.incoming <- value:
			default:
				connection.fail(errProtocol)
				return
			}
		}
	}
}

func (connection *client) write(ctx context.Context, value message) error {
	line, err := json.Marshal(value)
	if err != nil || len(line)+1 > maxMessage {
		return errProtocol
	}
	select {
	case connection.writes <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.ctx.Done():
		return connection.failure()
	}
	defer func() { <-connection.writes }()
	done := make(chan error, 1)
	go func() { _, err := connection.input.Write(append(line, '\n')); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			connection.fail(err)
		}
		return err
	case <-ctx.Done():
		connection.fail(ctx.Err())
		return ctx.Err()
	case <-connection.ctx.Done():
		return connection.failure()
	}
}

func (connection *client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	connection.mu.Lock()
	if len(connection.pending) >= maxPending || connection.next >= maxRequests {
		connection.mu.Unlock()
		return nil, errProtocol
	}
	connection.next++
	id := json.RawMessage(strconv.FormatInt(connection.next, 10))
	key, _ := requestID(id)
	response := make(chan message, 1)
	connection.pending[key] = response
	connection.mu.Unlock()
	defer func() { connection.mu.Lock(); delete(connection.pending, key); connection.mu.Unlock() }()
	if err := connection.write(ctx, message{ID: id, Method: method, Params: encoded}); err != nil {
		return nil, err
	}
	select {
	case value := <-response:
		if value.Error != nil {
			return nil, value.Error
		}
		return value.Result, nil
	case <-ctx.Done():
		connection.fail(ctx.Err())
		return nil, ctx.Err()
	case <-connection.ctx.Done():
		return nil, connection.failure()
	}
}
