package moonshine

import (
	"errors"
	"fmt"
)

// ErrorCode is a stable error code returned by the Moonshine C API.
type ErrorCode int32

const (
	ErrorUnknown         ErrorCode = -1
	ErrorInvalidHandle   ErrorCode = -2
	ErrorInvalidArgument ErrorCode = -3
)

// ErrClosed is returned when an operation requires a native resource that has
// already been closed.
var ErrClosed = errors.New("moonshine: resource is closed")

// Error describes a failure reported by the Moonshine native library.
type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message == "" {
		return fmt.Sprintf("moonshine error %d", e.Code)
	}
	return fmt.Sprintf("moonshine error %d: %s", e.Code, e.Message)
}

// Is matches native errors by code, allowing callers to use errors.Is with an
// Error value even when additional operation context has been wrapped around
// it.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && e != nil && other != nil && e.Code == other.Code
}

func nativeError(code int32, message string) error {
	if code >= 0 {
		return nil
	}
	if message == "" {
		message = fallbackErrorMessage(ErrorCode(code))
	}
	return &Error{Code: ErrorCode(code), Message: message}
}

func fallbackErrorMessage(code ErrorCode) string {
	switch code {
	case ErrorUnknown:
		return "Unknown error"
	case ErrorInvalidHandle:
		return "Invalid handle"
	case ErrorInvalidArgument:
		return "Invalid argument"
	default:
		return "Unknown error"
	}
}
