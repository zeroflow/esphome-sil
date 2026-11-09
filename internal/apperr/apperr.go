// Package apperr is the JSON error type of the sil CLI and its libraries.
//
// Exit classes: 1 is user-fixable (bad name, bad config, already running),
// 2 is infrastructure (docker, ports, sockets, internal failures).
package apperr

import (
	"errors"
	"fmt"
)

// Exit classes.
const (
	ExitFailed = 1
	ExitInfra  = 2
)

// Error is a machine-readable error with a stable code.
type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
	Exit    int            `json:"-"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Failed builds a user-fixable error (exit 1).
func Failed(code, msg string, detail map[string]any) *Error {
	return &Error{Code: code, Message: msg, Detail: detail, Exit: ExitFailed}
}

// Infra builds an infrastructure error (exit 2).
func Infra(code, msg string, detail map[string]any) *Error {
	return &Error{Code: code, Message: msg, Detail: detail, Exit: ExitInfra}
}

// ExitCode maps an error to a process exit code: nil -> 0, *Error -> its
// class, anything else -> 2.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *Error
	if errors.As(err, &e) && e != nil {
		return e.Exit
	}
	return ExitInfra
}

// AsError returns err as an *Error; foreign errors become
// Infra("internal", err.Error(), nil). A nil err yields nil.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) && e != nil {
		return e
	}
	return Infra("internal", err.Error(), nil)
}
