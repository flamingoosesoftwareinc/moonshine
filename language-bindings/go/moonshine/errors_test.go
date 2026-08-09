package moonshine

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeErrorKnownCodes(t *testing.T) {
	tests := []struct {
		name    string
		code    int32
		want    ErrorCode
		message string
	}{
		{name: "unknown", code: -1, want: ErrorUnknown, message: "Unknown error"},
		{name: "invalid handle", code: -2, want: ErrorInvalidHandle, message: "Invalid handle"},
		{name: "invalid argument", code: -3, want: ErrorInvalidArgument, message: "Invalid argument"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := nativeError(test.code, test.message)

			var nativeErr *Error
			require.ErrorAs(t, err, &nativeErr)
			assert.Equal(t, test.want, nativeErr.Code)
			assert.Equal(t, test.message, nativeErr.Message)
		})
	}
}

func TestNativeErrorUsesFallbackMessage(t *testing.T) {
	err := nativeError(-3, "")

	assert.EqualError(t, err, "moonshine error -3: Invalid argument")
}

func TestNativeErrorPreservesUnknownCode(t *testing.T) {
	err := nativeError(-99, "Future native error")

	var nativeErr *Error
	require.ErrorAs(t, err, &nativeErr)
	assert.Equal(t, ErrorCode(-99), nativeErr.Code)
	assert.Equal(t, "Future native error", nativeErr.Message)
}

func TestNativeErrorIgnoresNonErrors(t *testing.T) {
	assert.NoError(t, nativeError(0, ""))
	assert.NoError(t, nativeError(42, ""))
}

func TestErrorMatchesByCodeThroughWrapping(t *testing.T) {
	err := fmt.Errorf("load model: %w", nativeError(-2, "native detail"))

	assert.ErrorIs(t, err, &Error{Code: ErrorInvalidHandle})
	assert.NotErrorIs(t, err, &Error{Code: ErrorInvalidArgument})
	assert.False(t, errors.Is(err, ErrClosed))
}

func TestNilErrorString(t *testing.T) {
	var err *Error
	assert.Equal(t, "<nil>", err.Error())
}

func TestCopyCString(t *testing.T) {
	text := append([]byte("native message"), 0)

	assert.Equal(t, "native message", copyCString(&text[0]))
	assert.Empty(t, copyCString(nil))
}
