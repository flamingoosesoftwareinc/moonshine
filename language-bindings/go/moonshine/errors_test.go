package moonshine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeErrorKnownCodes(t *testing.T) {
	tests := []struct {
		name    string
		code    int32
		want    error
		message string
	}{
		{name: "unknown", code: -1, want: ErrUnknown, message: "Unknown error"},
		{name: "invalid handle", code: -2, want: ErrInvalidHandle, message: "Invalid handle"},
		{name: "invalid argument", code: -3, want: ErrInvalidArgument, message: "Invalid argument"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := nativeError(test.code, test.message)

			require.ErrorIs(t, err, test.want)
		})
	}
}

func TestNativeErrorUsesFallbackMessage(t *testing.T) {
	err := nativeError(-3, "")

	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestNativeErrorPreservesUnknownCode(t *testing.T) {
	err := nativeError(-99, "Future native error")

	require.ErrorIs(t, err, ErrUnknown)
}

func TestNativeErrorIgnoresNonErrors(t *testing.T) {
	assert.NoError(t, nativeError(0, ""))
	assert.NoError(t, nativeError(42, ""))
}

func TestSentinelsAreDistinct(t *testing.T) {
	assert.False(t, errors.Is(ErrUnknown, ErrInvalidHandle))
	assert.False(t, errors.Is(ErrInvalidHandle, ErrInvalidArgument))
	assert.False(t, errors.Is(ErrInvalidArgument, ErrClosed))
}

func TestCopyCString(t *testing.T) {
	text := append([]byte("native message"), 0)

	assert.Equal(t, "native message", copyCString(&text[0]))
	assert.Empty(t, copyCString(nil))
}
