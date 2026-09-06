package vulkan

import (
	"fmt"
	"reflect"
	"unsafe"
)

// The address and byte length of a block the caller wants memcpyd
//
// Upload and UpdateBuffer take `any` so one method serves every block the scene
// invents. A pointer or a slice is required rather than a bare struct: only
// those have an address Go will hand out, and asking for one is cheaper than
// the copy the alternative would make.
func dataPtr(data any) (unsafe.Pointer, uint64) { // TODO: review
	value := reflect.ValueOf(data)
	switch value.Kind() {
	case reflect.Slice:
		if value.Len() == 0 {
			return nil, 0
		}
		return value.UnsafePointer(), uint64(value.Len()) * uint64(value.Type().Elem().Size())
	case reflect.Pointer:
		if value.IsNil() {
			return nil, 0
		}
		return value.UnsafePointer(), uint64(value.Type().Elem().Size())
	default:
		panic(fmt.Sprintf("vulkan: uploads take a pointer or a slice, got %T", data))
	}
}

// Raw byte copy, the one operation every upload path reduces to
func memcpy(dst, src unsafe.Pointer, n uint64) { // TODO: review
	if n == 0 || dst == nil || src == nil {
		return
	}
	copy(unsafe.Slice((*byte)(dst), n), unsafe.Slice((*byte)(src), n))
}
