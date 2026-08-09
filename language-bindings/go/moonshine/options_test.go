package moonshine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOptions(t *testing.T) {
	tests := []struct {
		name    string
		options []Option
		wantErr bool
	}{
		{name: "none"},
		{name: "one", options: []Option{{Name: "word_timestamps", Value: "true"}}},
		{name: "empty value", options: []Option{{Name: "value", Value: ""}}},
		{name: "unicode", options: []Option{{Name: "language", Value: "日本語"}}},
		{name: "empty name", options: []Option{{Value: "true"}}, wantErr: true},
		{name: "NUL in name", options: []Option{{Name: "bad\x00name", Value: "true"}}, wantErr: true},
		{name: "NUL in value", options: []Option{{Name: "name", Value: "bad\x00value"}}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateOptions(test.options)
			if test.wantErr {
				require.ErrorIs(t, err, ErrInvalidArgument)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRawOptions(t *testing.T) {
	options := []Option{
		{Name: "word_timestamps", Value: "true"},
		{Name: "language", Value: "日本語"},
	}

	converted := rawOptions(options)

	require.Len(t, converted, 2)
	assert.Equal(t, append([]byte("word_timestamps"), 0), converted[0].Name)
	assert.Equal(t, append([]byte("true"), 0), converted[0].Value)
	assert.Equal(t, append([]byte("language"), 0), converted[1].Name)
	assert.Equal(t, append([]byte("日本語"), 0), converted[1].Value)
}

func TestRawOptionsEmptyIsNil(t *testing.T) {
	assert.Nil(t, rawOptions(nil))
	assert.Nil(t, rawOptions([]Option{}))
}
