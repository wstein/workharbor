// Package api is the JSON API of `whr serve` (design §9.7). Handlers decode,
// call the service and encode; they hold no logic of their own. The contract is
// openapi.json, and a test fails when a route and the document disagree.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/wstein/workharbor/internal/exitcode"
)

// SchemaVersion is the version of the response envelope (design §9.2).
const SchemaVersion = 1

// Envelope is every response: data on success, an error otherwise.
type Envelope struct {
	SchemaVersion int        `json:"schema_version"`
	OK            bool       `json:"ok"`
	Data          any        `json:"data,omitempty"`
	Error         *ErrorBody `json:"error,omitempty"`
}

// ErrorBody is the error of a failed request. The exit code is the CLI's, so a
// script sees the same number from the API and from `whr`.
type ErrorBody struct {
	Code     string `json:"code"`
	ExitCode int    `json:"exit_code"`
	Message  string `json:"message"`
}

// codeName names an exit code in the error body.
func codeName(code int) string {
	switch code {
	case exitcode.Usage:
		return "usage"
	case exitcode.NotFound:
		return "not_found"
	case exitcode.Auth:
		return "unauthorized"
	case exitcode.Conflict:
		return "conflict"
	case exitcode.NeedsHuman:
		return "needs_human"
	case exitcode.Timeout:
		return "timeout"
	case exitcode.TaskFailed:
		return "task_failed"
	}
	return "error"
}

// statusFor maps an exit code to the HTTP status of the response.
func statusFor(code int) int {
	switch code {
	case exitcode.Usage:
		return http.StatusBadRequest
	case exitcode.NotFound:
		return http.StatusNotFound
	case exitcode.Auth:
		return http.StatusUnauthorized
	case exitcode.Conflict, exitcode.NeedsHuman:
		return http.StatusConflict
	case exitcode.Timeout:
		return http.StatusGatewayTimeout
	}
	return http.StatusInternalServerError
}

// usageError is a request the API refuses before the service: a bad body or
// parameter. It is exit code 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }
func (usageError) ExitCode() int   { return exitcode.Usage }

// authError is a missing or wrong token. It is exit code 4.
type authError struct{}

func (authError) Error() string { return "a valid API token is required" }
func (authError) ExitCode() int { return exitcode.Auth }

func writeJSON(w http.ResponseWriter, status int, env Envelope) {
	env.SchemaVersion = SchemaVersion
	body, err := json.Marshal(env)
	if err != nil { // a value the encoder cannot handle is a bug, not a client error
		status = http.StatusInternalServerError
		body = []byte(`{"schema_version":1,"ok":false,"error":{"code":"error","exit_code":1,"message":"the response could not be encoded"}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

// writeOK writes a successful envelope.
func writeOK(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, Envelope{OK: true, Data: data})
}

// writeError writes the envelope of an error, with the status its exit code
// maps to. An error that is not a coded one is a 500 whose message is generic:
// what went wrong inside is for the log, not for the client.
func writeError(w http.ResponseWriter, err error) {
	code := exitcode.From(err)
	msg := err.Error()
	if code == exitcode.Error {
		var c exitcode.Coder
		if !errors.As(err, &c) {
			msg = "internal error"
		}
	}
	writeJSON(w, statusFor(code), Envelope{Error: &ErrorBody{Code: codeName(code), ExitCode: code, Message: msg}})
}
