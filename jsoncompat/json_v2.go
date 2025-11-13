//go:build goexperiment.jsonv2

package json

import (
	jsonv2 "encoding/json/v2"
	"io"
)

// JSON v2 compatibility layer

// Decoder wraps v2 unmarshal to provide v1-like Decode interface
type Decoder struct {
	r io.Reader
}

func (d *Decoder) Decode(v any) error {
	data, err := io.ReadAll(d.r)
	if err != nil {
		return err
	}
	return jsonv2.Unmarshal(data, v)
}

// Encoder wraps v2 marshal to provide v1-like Encode interface
type Encoder struct {
	w io.Writer
}

func (e *Encoder) Encode(v any) error {
	data, err := jsonv2.Marshal(v)
	if err != nil {
		return err
	}
	_, err = e.w.Write(data)
	return err
}

// RawMessage is a raw encoded JSON value.
// In v2, we use []byte as the closest equivalent to v1's json.RawMessage.
type RawMessage = []byte

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

func Unmarshal(data []byte, v any) error {
	return jsonv2.Unmarshal(data, v)
}

func Marshal(v any) ([]byte, error) {
	return jsonv2.Marshal(v)
}
