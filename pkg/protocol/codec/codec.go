package codec

import (
	"bytes"
	"encoding/gob"
	"fmt"
)

type Codec[T any] struct {
	encodable *T
}

func NewCodec[T any]() *Codec[T] {
	var x T
	return &Codec[T]{
		encodable: &x,
	}
}

func (c *Codec[T]) Encode(x T) ([]byte, error) {
	return Encode(x)
}

func (c *Codec[T]) Decode(data []byte) (*T, error) {
	return Decode[T](data)
}

type Encoder[T any] struct {
	buffer    bytes.Buffer
	encodable T
}

// data, err := codec.NewEncoder(t).Encode()
func NewEncoder[T any](x T) *Encoder[T] {
	return &Encoder[T]{
		encodable: x,
	}
}

func (e *Encoder[T]) Encode() ([]byte, error) {
	enc := gob.NewEncoder(&e.buffer)
	err := enc.Encode(e.encodable)
	if err != nil {
		return nil, fmt.Errorf("error while encoding: %w", err)
	}
	return e.buffer.Bytes(), nil
}

type Decoder[T any] struct {
	data      []byte
	decodable *T
}

// err := codec.NewDecoder(data, &t).Decode()
func NewDecoder[T any](data []byte, x *T) *Decoder[T] {
	return &Decoder[T]{
		data:      data,
		decodable: x,
	}
}

func (d *Decoder[T]) Decode() error {
	r := bytes.NewReader(d.data)
	enc := gob.NewDecoder(r) // Will write to network.
	pil := d.decodable
	err := enc.Decode(pil)
	if err != nil {
		return fmt.Errorf("error while decoding : %w", err)
	}
	return nil
}

// data, err := codec.NewEncoder(t).Encode()

func Encode[T any](pil T) ([]byte, error) {
	var buffer bytes.Buffer
	enc := gob.NewEncoder(&buffer)
	err := enc.Encode(pil)
	if err != nil {
		return nil, fmt.Errorf("error while encoding: %w", err)
	}
	return buffer.Bytes(), nil
}

func Decode[T any](data []byte) (*T, error) {
	var x T
	r := bytes.NewReader(data)
	enc := gob.NewDecoder(r) // Will write to network.
	pil := &x
	err := enc.Decode(pil)
	if err != nil {
		return nil, fmt.Errorf("error while decoding : %w", err)
	}
	return pil, nil
}
