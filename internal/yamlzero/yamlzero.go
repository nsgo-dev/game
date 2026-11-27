// Package yamlzero normalizes IEEE 754 negative zero (-0.0) floats to positive
// zero (0.0) in Go values decoded from or encoded to YAML.
//
// go.yaml.in/yaml/v3 formats a negative zero float as the plain scalar "-0",
// which YAML reads back as the integer 0, causing the next save to rewrite the
// field as "0". Normalizing on parse keeps parsed values free of negative zero
// in memory, and cleaning on encode guarantees idempotent round trips without
// mutating caller-owned values.
package yamlzero

import (
	"math"
	"reflect"
)

const maxDepth = 128

// Has reports whether v contains any reachable float32 or float64 negative zero.
// It never mutates v and does not allocate when inspecting structs, pointers,
// slices, arrays, or interfaces.
func Has(v any) bool {
	if v == nil {
		return false
	}
	return has(reflect.ValueOf(v), 0)
}

// Normalize replaces every reachable negative zero float32 and float64 in v
// with positive zero in place. Pass a pointer (or a slice/map) to mutate
// top-level values. When no negative zero is present in maps or interfaces,
// Normalize does not allocate.
func Normalize(v any) {
	if v == nil {
		return
	}
	normalize(reflect.ValueOf(v), 0)
}

// Clean returns v unchanged when it contains no negative zero float32 or
// float64. Otherwise it returns a copy along the paths containing negative
// zero with every negative zero replaced by positive zero, without mutating v.
func Clean[T any](v T) T {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || !has(rv, 0) {
		return v
	}
	return clean(rv, 0).Interface().(T)
}

func isNegZero(f float64) bool {
	return f == 0 && math.Signbit(f)
}

func has(rv reflect.Value, depth int) bool {
	if !rv.IsValid() || depth > maxDepth {
		return false
	}
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		return isNegZero(rv.Float())
	case reflect.Pointer:
		if rv.IsNil() {
			return false
		}
		return has(rv.Elem(), depth+1)
	case reflect.Interface:
		if rv.IsNil() {
			return false
		}
		return has(rv.Elem(), depth+1)
	case reflect.Struct:
		rt := rv.Type()
		for i := range rv.NumField() {
			sf := rt.Field(i)
			if !sf.IsExported() && !sf.Anonymous {
				continue
			}
			if has(rv.Field(i), depth+1) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range rv.Len() {
			if has(rv.Index(i), depth+1) {
				return true
			}
		}
	case reflect.Map:
		if rv.Len() == 0 {
			return false
		}
		iter := rv.MapRange()
		for iter.Next() {
			if has(iter.Value(), depth+1) {
				return true
			}
		}
	}
	return false
}

func normalize(rv reflect.Value, depth int) {
	if !rv.IsValid() || depth > maxDepth {
		return
	}
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		if rv.CanSet() && isNegZero(rv.Float()) {
			rv.SetFloat(0)
		}
	case reflect.Pointer:
		if !rv.IsNil() {
			normalize(rv.Elem(), depth+1)
		}
	case reflect.Interface:
		if rv.IsNil() {
			return
		}
		elem := rv.Elem()
		switch elem.Kind() {
		case reflect.Float32, reflect.Float64:
			if rv.CanSet() && isNegZero(elem.Float()) {
				rv.Set(reflect.Zero(elem.Type()))
			}
		case reflect.Pointer, reflect.Slice, reflect.Map:
			normalize(elem, depth+1)
		case reflect.Struct, reflect.Array:
			if rv.CanSet() && has(elem, depth+1) {
				cp := reflect.New(elem.Type()).Elem()
				cp.Set(elem)
				normalize(cp, depth+1)
				rv.Set(cp)
			}
		}
	case reflect.Struct:
		rt := rv.Type()
		for i := range rv.NumField() {
			sf := rt.Field(i)
			if !sf.IsExported() && !sf.Anonymous {
				continue
			}
			normalize(rv.Field(i), depth+1)
		}
	case reflect.Slice, reflect.Array:
		for i := range rv.Len() {
			normalize(rv.Index(i), depth+1)
		}
	case reflect.Map:
		if rv.Len() == 0 {
			return
		}
		iter := rv.MapRange()
		for iter.Next() {
			val := iter.Value()
			switch val.Kind() {
			case reflect.Float32, reflect.Float64:
				if isNegZero(val.Float()) {
					rv.SetMapIndex(iter.Key(), reflect.Zero(val.Type()))
				}
			case reflect.Pointer, reflect.Slice, reflect.Map:
				normalize(val, depth+1)
			case reflect.Struct, reflect.Array:
				if has(val, depth+1) {
					cp := reflect.New(val.Type()).Elem()
					cp.Set(val)
					normalize(cp, depth+1)
					rv.SetMapIndex(iter.Key(), cp)
				}
			case reflect.Interface:
				if !val.IsNil() && has(val, depth+1) {
					rv.SetMapIndex(iter.Key(), clean(val, depth+1))
				}
			}
		}
	}
}

func clean(rv reflect.Value, depth int) reflect.Value {
	if !rv.IsValid() || depth > maxDepth || !has(rv, depth) {
		return rv
	}
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		return reflect.Zero(rv.Type())
	case reflect.Pointer:
		dst := reflect.New(rv.Type().Elem())
		dst.Elem().Set(clean(rv.Elem(), depth+1))
		return dst
	case reflect.Interface:
		return clean(rv.Elem(), depth+1)
	case reflect.Struct:
		dst := reflect.New(rv.Type()).Elem()
		dst.Set(rv)
		rt := rv.Type()
		for i := range rv.NumField() {
			sf := rt.Field(i)
			if !sf.IsExported() && !sf.Anonymous {
				continue
			}
			f := rv.Field(i)
			if has(f, depth+1) {
				dstField := dst.Field(i)
				if dstField.CanSet() {
					dstField.Set(clean(f, depth+1))
				} else if sf.Anonymous && f.Kind() == reflect.Struct {
					// Embedded unexported struct: descend into its exported fields.
					cleanEmbeddedStruct(dstField, f, depth+1)
				}
			}
		}
		return dst
	case reflect.Array:
		dst := reflect.New(rv.Type()).Elem()
		dst.Set(rv)
		for i := range rv.Len() {
			elem := rv.Index(i)
			if has(elem, depth+1) {
				dst.Index(i).Set(clean(elem, depth+1))
			}
		}
		return dst
	case reflect.Slice:
		dst := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		reflect.Copy(dst, rv)
		for i := range rv.Len() {
			elem := rv.Index(i)
			if has(elem, depth+1) {
				dst.Index(i).Set(clean(elem, depth+1))
			}
		}
		return dst
	case reflect.Map:
		dst := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			val := iter.Value()
			if has(val, depth+1) {
				val = clean(val, depth+1)
			}
			dst.SetMapIndex(iter.Key(), val)
		}
		return dst
	default:
		return rv
	}
}

func cleanEmbeddedStruct(dst, src reflect.Value, depth int) {
	if depth > maxDepth {
		return
	}
	rt := src.Type()
	for i := range src.NumField() {
		sf := rt.Field(i)
		if !sf.IsExported() && !sf.Anonymous {
			continue
		}
		f := src.Field(i)
		if has(f, depth+1) {
			df := dst.Field(i)
			if df.CanSet() {
				df.Set(clean(f, depth+1))
			} else if sf.Anonymous && f.Kind() == reflect.Struct {
				cleanEmbeddedStruct(df, f, depth+1)
			}
		}
	}
}
