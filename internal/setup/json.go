package setup

import (
	"bytes"
	"encoding/json"
)

// Obj is a JSON object that keeps its keys in insertion order, like a JavaScript object literal.
type Obj []KV

// KV is one key of an Obj.
type KV struct {
	K string
	V any
}

// Get returns the value under key, or nil.
func (o Obj) Get(key string) any {
	for _, kv := range o {
		if kv.K == key {
			return kv.V
		}
	}
	return nil
}

// MarshalJSON writes the keys in order.
func (o Obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := marshal(kv.K)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		v, err := marshal(kv.V)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshal is json.Marshal without HTML escaping, as JSON.stringify writes `<` and `>`.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// Stringify is JSON.stringify: compact, keys in order, `<`, `>` and `&` as they are.
func Stringify(v any) string {
	out, err := marshal(v)
	if err != nil {
		panic(err)
	}
	return string(out)
}
