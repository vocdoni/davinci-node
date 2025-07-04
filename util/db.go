package util

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/vocdoni/davinci-node/log"
)

// HandleClosedDBPanic wraps the commit operation to handle panic scenarios
// about already closed database. It should be deferred in any storage
// operation that might read, write or commit to the database that can be
// closed during the operation.
func HandleClosedDBPanic() {
	if r := recover(); r != nil {
		// Collect stack trace
		stack := []string{}
		for i := range 32 {
			pc, file, line, ok := runtime.Caller(i)
			if !ok {
				break
			}
			fn := runtime.FuncForPC(pc)
			funcName := ""
			if fn != nil {
				funcName = fn.Name()
			}
			stack = append(stack, fmt.Sprintf("%s\n\t%s:%d", funcName, file, line))
		}

		// Check if the panic is due to a closed database
		if strings.Contains(fmt.Sprintf("%v", r), "closed") {
			// Log the warning with the stack trace
			log.Warnw("database already closed", "error", r, "stack", stack)
			return
		}
		// If it's not a closed database panic, re-panic with the stack trace
		panic(fmt.Sprintf("panic during storage operation: %v: %s", r, strings.Join(stack, "\n")))
	}
}
