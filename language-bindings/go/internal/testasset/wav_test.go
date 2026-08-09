package testasset

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeWAV(t *testing.T) {
	wav := pcm16WAV(16000, []int16{-32768, 0, 16384, 32767})

	decoded, err := DecodeWAV(bytes.NewReader(wav))

	require.NoError(t, err)
	assert.Equal(t, 16000, decoded.SampleRate)
	assert.InDeltaSlice(t, []float32{-1, 0, 0.5, 0.9999695}, decoded.Samples, 0.000001)
}

func TestDecodeWAVRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "truncated", data: []byte("RIFF")},
		{name: "not WAV", data: []byte("not a WAV file")},
		{name: "no data", data: pcm16WAV(16000, nil)[:36]},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeWAV(bytes.NewReader(test.data))
			require.Error(t, err)
		})
	}
}

func pcm16WAV(sampleRate uint32, samples []int16) []byte {
	dataSize := uint32(len(samples) * 2)
	result := new(bytes.Buffer)
	result.WriteString("RIFF")
	_ = binary.Write(result, binary.LittleEndian, uint32(36)+dataSize)
	result.WriteString("WAVEfmt ")
	_ = binary.Write(result, binary.LittleEndian, uint32(16))
	_ = binary.Write(result, binary.LittleEndian, uint16(1))
	_ = binary.Write(result, binary.LittleEndian, uint16(1))
	_ = binary.Write(result, binary.LittleEndian, sampleRate)
	_ = binary.Write(result, binary.LittleEndian, sampleRate*2)
	_ = binary.Write(result, binary.LittleEndian, uint16(2))
	_ = binary.Write(result, binary.LittleEndian, uint16(16))
	result.WriteString("data")
	_ = binary.Write(result, binary.LittleEndian, dataSize)
	for _, sample := range samples {
		_ = binary.Write(result, binary.LittleEndian, sample)
	}
	return result.Bytes()
}
