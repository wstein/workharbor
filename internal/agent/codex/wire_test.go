package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestEnvelope(t *testing.T) {
	for _, input := range []string{
		`null`, `{}`, `{"id":null,"result":{}}`, `{"id":1,"result":{},"error":{}}`,
		`{"id":1,"id":2,"result":{}}`, `{"method":"x","params":{"x":1,"x":2}}`,
		`{"id":true,"result":{}}`, `{"id":1.5,"result":{}}`, `{"method":"x"}`, `{"method":"x","params":[]} `,
		`{"id":1,"result":{}} {}`, strings.Repeat("[", 65) + strings.Repeat("]", 65),
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := decodeMessage([]byte(input)); !errors.Is(err, errProtocol) {
				t.Fatalf("accepted malformed envelope: %v", err)
			}
		})
	}
	for _, input := range []string{`{"id":0,"result":{}}`, `{"id":"request","method":"x","params":{}}`, `{"method":"x","params":{}}`} {
		if _, err := decodeMessage([]byte(input)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClientCorrelation(t *testing.T) {
	host, server := net.Pipe()
	defer server.Close()
	client := newClient(context.Background(), host, host)
	defer client.close()
	go func() {
		reader := bufio.NewReader(server)
		line, _ := reader.ReadBytes('\n')
		var request message
		_ = json.Unmarshal(line, &request)
		_, _ = server.Write([]byte(`{"method":"thread/started","params":{}}` + "\n"))
		response, _ := json.Marshal(message{ID: request.ID, Result: json.RawMessage(`{"turnId":"turn"}`)})
		_, _ = server.Write(append(response, '\n'))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := client.call(ctx, "turn/steer", map[string]string{"threadId": "thread"})
	if err != nil || string(result) != `{"turnId":"turn"}` {
		t.Fatalf("result %s, error %v", result, err)
	}
	select {
	case notification := <-client.incoming:
		if notification.Method != "thread/started" {
			t.Fatal(notification)
		}
	case <-ctx.Done():
		t.Fatal("notification lost")
	}
}

func TestClientFailsClosed(t *testing.T) {
	for _, input := range []string{`{"id":999,"result":{}}` + "\n", strings.Repeat("x", maxMessage+1), `{"method":"x","params":{}}`} {
		t.Run(input[:min(len(input), 40)], func(t *testing.T) {
			host, server := net.Pipe()
			client := newClient(context.Background(), host, host)
			defer client.close()
			go func() { _, _ = server.Write([]byte(input)); _ = server.Close() }()
			select {
			case <-client.ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("malformed stream did not stop client")
			}
			if !errors.Is(client.failure(), errProtocol) {
				t.Fatal(client.failure())
			}
		})
	}
}

func TestClientCancelledWrite(t *testing.T) {
	host, server := net.Pipe()
	defer server.Close()
	client := newClient(context.Background(), host, host)
	defer client.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := client.call(ctx, "initialize", map[string]any{}); err == nil {
		t.Fatal("blocked write succeeded")
	}
	select {
	case <-client.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop connection")
	}
}
