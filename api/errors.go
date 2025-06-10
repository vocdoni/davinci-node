package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/vocdoni/davinci-node/log"
)

// Error is used by handler functions to wrap errors, assigning a unique error code
// and also specifying which HTTP Status should be used.
type Error struct {
	Err        error
	Code       int
	HTTPstatus int
}

// MarshalJSON returns a JSON containing Err.Error() and Code. Field HTTPstatus is ignored.
//
// Example output: {"error":"account not found","code":4003}
func (e Error) MarshalJSON() ([]byte, error) {
	// This anon struct is needed to actually include the error string,
	// since it wouldn't be marshaled otherwise. (json.Marshal doesn't call Err.Error())
	return json.Marshal(
		struct {
			Err  string `json:"error"`
			Code int    `json:"code"`
		}{
			Err:  e.Err.Error(),
			Code: e.Code,
		})
}

// Error returns the Message contained inside the APIerror
func (e Error) Error() string {
	return e.Err.Error()
}

// Write serializes a JSON msg using APIerror.Message and APIerror.Code
// and passes that to ctx.Send()
func (e Error) Write(w http.ResponseWriter) {
	msg, err := json.Marshal(e)
	if err != nil {
		log.Warn(err)
		http.Error(w, "marshal failed", http.StatusInternalServerError)
		return
	}
	if log.Level() == log.LogLevelDebug {
		log.Debugw("API error response", "error", e.Error(), "code", e.Code, "httpStatus", e.HTTPstatus)
	}
	// set the content type to JSON
	w.Header().Set("Content-Type", "application/json")
	http.Error(w, string(msg), e.HTTPstatus)
}

// Withf returns a copy of APIerror with the Sprintf formatted string appended at the end of e.Err
func (e Error) Withf(format string, args ...any) Error {
	return Error{
		Err:        fmt.Errorf("%w: %v", e.Err, fmt.Sprintf(format, args...)),
		Code:       e.Code,
		HTTPstatus: e.HTTPstatus,
	}
}

// With returns a copy of APIerror with the string appended at the end of e.Err
func (e Error) With(s string) Error {
	return Error{
		Err:        fmt.Errorf("%w: %v", e.Err, s),
		Code:       e.Code,
		HTTPstatus: e.HTTPstatus,
	}
}

// WithErr returns a copy of APIerror with err.Error() appended at the end of e.Err
func (e Error) WithErr(err error) Error {
	return Error{
		Err:        fmt.Errorf("%w: %v", e.Err, err.Error()),
		Code:       e.Code,
		HTTPstatus: e.HTTPstatus,
	}
}
