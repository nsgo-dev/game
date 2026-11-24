// Package jsonedit strictly decodes JSON edits onto live Go values without touching the edited value until the edit has succeeded.
package jsonedit

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"io"
	"reflect"
)

// maxDepth bounds the recursion of detach and merge so a value that refers to itself through interfaces or slices cannot recurse forever.
const maxDepth = 1000

var (
	jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// DecodeStrict decodes exactly one JSON value from data into dst, refusing unknown object fields and trailing data.
func DecodeStrict(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("unexpected trailing data")
	}
	return nil
}

// Patch strictly decodes data onto a copy of *cur and returns the edited value, leaving *cur and everything it refers to unchanged whether or not the decode succeeds.
//
// The decode runs on a clone of *cur whose exported pointers, maps, slices and interfaces are freshly allocated, so a decode that fails halfway cannot reach the live value. The result then starts from a plain copy of *cur and takes from the decoded clone only the exported fields the patch changed, so unexported fields, fields tagged `json:"-"` and the identity of every pointer, map and slice the patch left alone survive. A field the patch changed holds a freshly allocated value. A type that implements json.Unmarshaler or encoding.TextUnmarshaler is taken whole from the decoded clone, because its own decoding may set fields Patch cannot see.
func Patch[T any](cur *T, data []byte) (T, error) {
	decoded := *cur
	dv := reflect.ValueOf(&decoded).Elem()
	detach(dv, make(map[visit]reflect.Value), 0)
	if err := DecodeStrict(data, &decoded); err != nil {
		var zero T
		return zero, err
	}
	out := *cur
	merge(reflect.ValueOf(&out).Elem(), reflect.ValueOf(cur).Elem(), dv, 0)
	return out, nil
}

type visit struct {
	ptr uintptr
	typ reflect.Type
}

// detach replaces every settable pointer, map, slice and interface reachable from the addressable value v with a fresh copy, so decoding into v cannot write to memory shared with the value v was copied from.
func detach(v reflect.Value, seen map[visit]reflect.Value, depth int) {
	if depth >= maxDepth {
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			detach(v.Field(i), seen, depth+1)
		}
	case reflect.Array:
		for i := range v.Len() {
			detach(v.Index(i), seen, depth+1)
		}
	case reflect.Pointer:
		if v.IsNil() || !v.CanSet() {
			return
		}
		key := visit{ptr: v.Pointer(), typ: v.Type()}
		if c, ok := seen[key]; ok {
			v.Set(c)
			return
		}
		np := reflect.New(v.Type().Elem())
		seen[key] = np
		np.Elem().Set(v.Elem())
		detach(np.Elem(), seen, depth+1)
		v.Set(np)
	case reflect.Map:
		if v.IsNil() || !v.CanSet() {
			return
		}
		key := visit{ptr: v.Pointer(), typ: v.Type()}
		if c, ok := seen[key]; ok {
			v.Set(c)
			return
		}
		nm := reflect.MakeMapWithSize(v.Type(), v.Len())
		seen[key] = nm
		iter := v.MapRange()
		for iter.Next() {
			ev := reflect.New(v.Type().Elem()).Elem()
			ev.Set(iter.Value())
			detach(ev, seen, depth+1)
			nm.SetMapIndex(iter.Key(), ev)
		}
		v.Set(nm)
	case reflect.Slice:
		if v.IsNil() || !v.CanSet() {
			return
		}
		ns := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(ns, v)
		for i := range ns.Len() {
			detach(ns.Index(i), seen, depth+1)
		}
		v.Set(ns)
	case reflect.Interface:
		if v.IsNil() || !v.CanSet() {
			return
		}
		inner := reflect.New(v.Elem().Type()).Elem()
		inner.Set(v.Elem())
		detach(inner, seen, depth+1)
		v.Set(inner)
	}
}

// merge sets each settable part of dst whose value differs between cur and decoded to the decoded value, descending into structs so unchanged sibling fields keep cur's values.
func merge(dst, cur, decoded reflect.Value, depth int) {
	if dst.Kind() == reflect.Struct && depth < maxDepth && !customUnmarshaler(dst.Type()) {
		for i := range dst.NumField() {
			merge(dst.Field(i), cur.Field(i), decoded.Field(i), depth+1)
		}
		return
	}
	if !dst.CanSet() {
		return
	}
	if !reflect.DeepEqual(cur.Interface(), decoded.Interface()) {
		dst.Set(decoded)
	}
}

func customUnmarshaler(t reflect.Type) bool {
	pt := reflect.PointerTo(t)
	return t.Implements(jsonUnmarshalerType) || pt.Implements(jsonUnmarshalerType) || t.Implements(textUnmarshalerType) || pt.Implements(textUnmarshalerType)
}
