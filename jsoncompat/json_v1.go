//go:build !goexperiment.jsonv2

package json

import (
	stdjson "encoding/json"
	"io"
)

// JSON v1 compatibility layer (default)

type (
	Decoder    = stdjson.Decoder
	Encoder    = stdjson.Encoder
	RawMessage = stdjson.RawMessage
)

func NewDecoder(r io.Reader) *Decoder {
	return stdjson.NewDecoder(r)
}

func NewEncoder(w io.Writer) *Encoder {
	return stdjson.NewEncoder(w)
}

func Unmarshal(data []byte, v any) error {
	return stdjson.Unmarshal(data, v)
}

func Marshal(v any) ([]byte, error) {
	return stdjson.Marshal(v)
}
