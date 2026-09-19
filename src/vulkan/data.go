package vulkan

import (
	"fmt"
	"reflect"
	"unsafe"
)

// Returns address and length in bytes of any block of data
func getDataPointer(data any) (unsafe.Pointer, uint64) { 
	// ValueOf returns a Value storing data type, length, element type and address
	value := reflect.ValueOf(data)
	switch value.Kind() {
		// Handle a non‑empty slice: give its pointer and total byte size
		case reflect.Slice:
			if value.Len() == 0 {
				return nil, 0
			}
			return value.UnsafePointer(), uint64(value.Len()) * uint64(value.Type().Elem().Size())
		// Handle a non‑nil pointer: give its pointer and the size of the pointed object
		case reflect.Pointer:
			if value.IsNil() {
				return nil, 0
			}
			return value.UnsafePointer(), uint64(value.Type().Elem().Size())
		default:
			// Reject any other kind: uploads must be a pointer or slice
			panic(fmt.Sprintf("vulkan: uploads take a pointer or a slice, got %T", data))
	}
}

// Raw byte copy, the one operation every upload path reduces to
func memoryCopy(dst, src unsafe.Pointer, n uint64) { 
	if n == 0 || dst == nil || src == nil {
		return
	}
	copy(unsafe.Slice((*byte)(dst), n), unsafe.Slice((*byte)(src), n))
}
