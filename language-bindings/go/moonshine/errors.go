package moonshine

import (
	"errors"
	"fmt"
)

var (
	// ErrUnknown represents an unspecified failure in the native library.
	ErrUnknown = errors.New("moonshine: unknown error")
	// ErrInvalidHandle indicates that a native resource handle is not valid.
	ErrInvalidHandle = errors.New("moonshine: invalid handle")
	// ErrInvalidArgument indicates that the native API rejected an argument.
	ErrInvalidArgument = errors.New("moonshine: invalid argument")
	// ErrClosed indicates that an operation requires a native resource which
	// has already been closed.
	ErrClosed = errors.New("moonshine: resource is closed")
)

func nativeError(code int32, message string) error {
	if code >= 0 {
		return nil
	}

	sentinel := sentinelForErrorCode(code)
	if message == "" {
		if sentinel != ErrUnknown || code == -1 {
			return sentinel
		}
		return fmt.Errorf("%w (native code %d)", sentinel, code)
	}
	if sentinel == ErrUnknown && code != -1 {
		return fmt.Errorf("%w (native code %d): %s", sentinel, code, message)
	}
	return fmt.Errorf("%w: %s", sentinel, message)
}

func sentinelForErrorCode(code int32) error {
	switch code {
	case -2:
		return ErrInvalidHandle
	case -3:
		return ErrInvalidArgument
	default:
		return ErrUnknown
	}
}
