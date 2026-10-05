package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// openSignInShell obtains metadata on a connection that holds the environment.
// Its context ends on EOF, malformed liveness, a stalled peer or cancellation.
// Closing it after the child exits releases the environment; no terminal data
// is sent or received here.
func (c *Client) openSignInShell(ctx context.Context, path string) (shellTarget, context.Context, func(), error) {
	held, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(held, http.MethodPost, c.base+path, nil)
	if err != nil {
		cancel()
		return shellTarget{}, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/x-ndjson")
	timer := time.AfterFunc(slowTimeout, cancel) // stop/start/readiness can take minutes
	hc := &http.Client{Transport: c.hc.Transport}
	resp, err := hc.Do(req) //nolint:bodyclose // closeHold owns the response body and is deferred by the caller and reader
	if err != nil {
		timer.Stop()
		cancel()
		return shellTarget{}, nil, nil, connError{redactURLError(err)}
	}
	closeHold := func() { timer.Stop(); cancel(); _ = resp.Body.Close() }
	scanner := bufio.NewScanner(resp.Body) // bounded frames, never terminal data
	if !scanner.Scan() {
		closeHold()
		return shellTarget{}, nil, nil, fmt.Errorf("the shell hold ended before its target")
	}
	var env envelope
	if err := json.Unmarshal(scanner.Bytes(), &env); err != nil {
		closeHold()
		return shellTarget{}, nil, nil, fmt.Errorf("the shell hold returned an invalid envelope")
	}
	if !env.OK || resp.StatusCode != http.StatusOK {
		closeHold()
		if env.Error != nil {
			return shellTarget{}, nil, nil, &RemoteError{Code: env.Error.Code, Exit: env.Error.ExitCode, Message: env.Error.Message}
		}
		return shellTarget{}, nil, nil, fmt.Errorf("the shell hold returned no target")
	}
	if resp.Header.Get("Content-Type") != "application/x-ndjson" {
		closeHold()
		return shellTarget{}, nil, nil, fmt.Errorf("the supervisor does not support a lifetime shell hold")
	}
	var target shellTarget
	if err := json.Unmarshal(env.Data, &target); err != nil {
		closeHold()
		return shellTarget{}, nil, nil, fmt.Errorf("the shell target is invalid")
	}
	timer.Reset(5 * time.Second)
	go func() {
		defer closeHold()
		for scanner.Scan() {
			if string(scanner.Bytes()) != `{"held":true}` {
				return
			}
			if held.Err() != nil {
				return
			}
			timer.Reset(5 * time.Second)
		}
	}()
	return target, held, closeHold, nil
}
