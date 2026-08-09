package moonshine

import (
	"fmt"
	"strings"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
)

// Option is an advanced Moonshine native API option.
type Option struct {
	Name  string
	Value string
}

func validateOptions(options []Option) error {
	for index, option := range options {
		if option.Name == "" {
			return fmt.Errorf("option %d has an empty name: %w", index, ErrInvalidArgument)
		}
		if strings.IndexByte(option.Name, 0) >= 0 {
			return fmt.Errorf("option %q contains a NUL in its name: %w", option.Name, ErrInvalidArgument)
		}
		if strings.IndexByte(option.Value, 0) >= 0 {
			return fmt.Errorf("option %q contains a NUL in its value: %w", option.Name, ErrInvalidArgument)
		}
	}
	return nil
}

func rawOptions(options []Option) []raw.MoonshineOptionT {
	if len(options) == 0 {
		return nil
	}

	converted := make([]raw.MoonshineOptionT, len(options))
	for index, option := range options {
		converted[index].Name = nulTerminated(option.Name)
		converted[index].Value = nulTerminated(option.Value)
	}
	return converted
}

func nulTerminated(value string) []byte {
	result := make([]byte, len(value)+1)
	copy(result, value)
	return result
}
