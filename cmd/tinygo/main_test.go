package main

import (
	"fmt"
	"testing"
	"unsafe"
)

func TestEncrypt(t *testing.T) {
	// Call encrypt with a simple value
	status := encrypt(42)

	if status != 1 {
		t.Fatalf("Encryption failed with status: %d", status)
	}

	// Use the getter functions to get the results
	fmt.Printf("Encryption result for input 42:\n")

	// Helper function to convert *byte to string
	cStringToGoString := func(cString *byte) string {
		if cString == nil {
			return ""
		}

		// Find the null terminator
		var length int
		for ptr := unsafe.Pointer(cString); *(*byte)(ptr) != 0; ptr = unsafe.Pointer(uintptr(ptr) + 1) {
			length++
		}

		// Convert to Go string
		bytes := make([]byte, length)
		for i := 0; i < length; i++ {
			bytes[i] = *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(cString)) + uintptr(i)))
		}

		return string(bytes)
	}

	// Get and print each component
	fmt.Println("Point 1:")
	fmt.Printf("  x: %s\n", cStringToGoString(getResultX1()))
	fmt.Printf("  y: %s\n", cStringToGoString(getResultY1()))
	fmt.Println("Point 2:")
	fmt.Printf("  x: %s\n", cStringToGoString(getResultX2()))
	fmt.Printf("  y: %s\n", cStringToGoString(getResultY2()))
}
