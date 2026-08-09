package testasset

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// WAV is decoded mono float PCM test audio.
type WAV struct {
	Samples    []float32
	SampleRate int
}

// LoadWAV loads a PCM WAV fixture from disk.
func LoadWAV(path string) (WAV, error) {
	file, err := os.Open(path)
	if err != nil {
		return WAV{}, err
	}
	defer file.Close()
	return DecodeWAV(file)
}

// DecodeWAV decodes mono 16-bit PCM WAV data.
func DecodeWAV(reader io.Reader) (WAV, error) {
	var header [12]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return WAV{}, fmt.Errorf("read WAV header: %w", err)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return WAV{}, fmt.Errorf("invalid RIFF/WAVE header")
	}

	var formatFound bool
	var sampleRate uint32
	var channels uint16
	var bitsPerSample uint16
	var samples []float32

	for {
		var chunkHeader [8]byte
		if _, err := io.ReadFull(reader, chunkHeader[:]); err != nil {
			if err == io.EOF {
				break
			}
			return WAV{}, fmt.Errorf("read WAV chunk header: %w", err)
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])
		if uint64(chunkSize) > uint64(math.MaxInt) {
			return WAV{}, fmt.Errorf("WAV chunk %q is too large", chunkID)
		}
		chunk := make([]byte, int(chunkSize))
		if _, err := io.ReadFull(reader, chunk); err != nil {
			return WAV{}, fmt.Errorf("read WAV chunk %q: %w", chunkID, err)
		}
		if chunkSize%2 != 0 {
			var padding [1]byte
			if _, err := io.ReadFull(reader, padding[:]); err != nil {
				return WAV{}, fmt.Errorf("read WAV chunk padding: %w", err)
			}
		}

		switch chunkID {
		case "fmt ":
			if len(chunk) < 16 {
				return WAV{}, fmt.Errorf("WAV fmt chunk is truncated")
			}
			if format := binary.LittleEndian.Uint16(chunk[0:2]); format != 1 {
				return WAV{}, fmt.Errorf("unsupported WAV format %d", format)
			}
			channels = binary.LittleEndian.Uint16(chunk[2:4])
			sampleRate = binary.LittleEndian.Uint32(chunk[4:8])
			bitsPerSample = binary.LittleEndian.Uint16(chunk[14:16])
			formatFound = true
		case "data":
			if !formatFound {
				return WAV{}, fmt.Errorf("WAV data precedes fmt chunk")
			}
			if channels != 1 {
				return WAV{}, fmt.Errorf("unsupported WAV channel count %d", channels)
			}
			if bitsPerSample != 16 {
				return WAV{}, fmt.Errorf("unsupported WAV sample width %d", bitsPerSample)
			}
			if len(chunk)%2 != 0 {
				return WAV{}, fmt.Errorf("WAV data contains a partial sample")
			}
			samples = make([]float32, len(chunk)/2)
			for index := range samples {
				value := int16(binary.LittleEndian.Uint16(chunk[index*2:]))
				samples[index] = float32(value) / 32768
			}
		}
	}

	if !formatFound {
		return WAV{}, fmt.Errorf("WAV has no fmt chunk")
	}
	if samples == nil {
		return WAV{}, fmt.Errorf("WAV has no data chunk")
	}
	if sampleRate == 0 || uint64(sampleRate) > uint64(math.MaxInt) {
		return WAV{}, fmt.Errorf("invalid WAV sample rate %d", sampleRate)
	}
	return WAV{Samples: samples, SampleRate: int(sampleRate)}, nil
}
