//go:build cgo

/*
	This dummy file avoids a `warning: fr_asm.o: missing .note.GNU-stack section implies executable stack`
	during circuit compilation.
*/

package circuits

/*
#cgo LDFLAGS: -Wl,-z,noexecstack
*/
import "C"
