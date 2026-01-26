package log

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"

	RFC3339Milli = "2006-01-02T15:04:05.000Z07:00" // like time.RFC3339Nano but with 3 fixed-width decimals
)

var (
	log   zerolog.Logger
	logMu sync.RWMutex
	// panicOnInvalidChars is set based on env LOG_PANIC_ON_INVALIDCHARS (parsed as bool)
	panicOnInvalidChars = os.Getenv("LOG_PANIC_ON_INVALIDCHARS") == "true"
)

func init() {
	// Allow overriding the default log level via $LOG_LEVEL, so that the
	// environment variable can be set globally even when running tests.
	// Always initializing the logger is also useful to avoid panics when
	// logging if the logger is nil.
	Init(cmp.Or(os.Getenv("LOG_LEVEL"), "error"), "stderr", nil)
}

// Logger provides access to the global logger (zerolog).
func Logger() *zerolog.Logger {
	logger := getLogger()
	return &logger
}

func getLogger() zerolog.Logger {
	logMu.RLock()
	logger := log
	logMu.RUnlock()
	return logger
}

func setLogger(logger zerolog.Logger) {
	logMu.Lock()
	log = logger
	logMu.Unlock()
}

var logTestWriter io.Writer // for TestLogger
const logTestWriterName = "log_test_writer"

// logTestTime is used to ensure that the log output in the test is deterministic.
var logTestTime, _ = time.Parse(RFC3339Milli, "2006-01-02T15:04:05.000Z")

// panicOnErrorHook panics when encountering Error level logs.
// This is useful for integration tests to catch unexpected errors.
type panicOnErrorHook struct {
	TestName string
	Delay    time.Duration
	Handler  func(string)
	once     sync.Once
}

// Run panics if the log level is Error or higher.
func (h *panicOnErrorHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	if level >= zerolog.ErrorLevel {
		panicMsg := fmt.Sprintf("ERROR found in logs during test %s: %s", h.TestName, msg)
		h.once.Do(func() {
			delay := h.Delay
			if delay <= 0 {
				delay = time.Second
			}
			handler := h.Handler
			if handler == nil {
				handler = func(message string) { panic(message) }
			}
			time.AfterFunc(delay, func() {
				handler(panicMsg)
			})
		})
	}
}

// EnablePanicOnError installs a hook on the current logger
// that makes it panic when Error level logs occur.
// Returns the previous logger so it can be restored later.
// This is useful for integration tests to catch unexpected errors.
func EnablePanicOnError(testName string) zerolog.Logger {
	return EnablePanicOnErrorWithHandler(testName, time.Second, nil)
}

// EnablePanicOnErrorWithHandler installs a hook on the current logger that
// triggers the handler after the provided delay when Error level logs occur.
// If handler is nil, it panics with the error message.
func EnablePanicOnErrorWithHandler(testName string, delay time.Duration, handler func(string)) zerolog.Logger {
	previousLogger := getLogger()
	setLogger(previousLogger.Hook(&panicOnErrorHook{
		TestName: testName,
		Delay:    delay,
		Handler:  handler,
	}))
	return previousLogger
}

// RestoreLogger restores a previously saved logger, removing any hooks.
func RestoreLogger(previousLogger zerolog.Logger) {
	setLogger(previousLogger)
}

type errorLevelWriter struct {
	io.Writer
}

var _ zerolog.LevelWriter = &errorLevelWriter{}

func (*errorLevelWriter) Write(_ []byte) (int, error) {
	panic("should be calling WriteLevel")
}

func (w *errorLevelWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	if level < zerolog.WarnLevel {
		return len(p), nil
	}
	return w.Writer.Write(p)
}

// invalidCharChecker checks if the formatted string contains the Unicode replacement char (U+FFFD)
// and panics if env LOG_PANIC_ON_INVALIDCHARS bool is true.
//
// In production (LOG_PANIC_ON_INVALIDCHARS != true), this function returns immediately,
// i.e. no performance hit
//
// If the log string contains the "replacement char"
// https://en.wikipedia.org/wiki/Specials_(Unicode_block)#Replacement_character
// this most likely means a bug in the caller (a format mismatch in fmt.Sprintf())
type invalidCharChecker struct{}

func (*invalidCharChecker) Write(p []byte) (int, error) {
	if bytes.ContainsRune(p, '\uFFFD') {
		panic(fmt.Sprintf("log line with invalid chars: %q", string(p)))
	}
	return len(p), nil
}

func Init(level, output string, errorOutput io.Writer) {
	var out io.Writer
	outputs := []io.Writer{}
	switch output {
	case "stdout":
		out = os.Stdout
	case "stderr":
		out = os.Stderr
	case logTestWriterName:
		out = logTestWriter
	default:
		f, err := os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			panic(fmt.Sprintf("cannot create log output: %v", err))
		}
		out = f
		if strings.HasSuffix(output, ".json") {
			outputs = append(outputs, f)
			out = os.Stdout
		}
	}
	out = zerolog.ConsoleWriter{
		Out:        out,
		TimeFormat: RFC3339Milli,
	}
	outputs = append(outputs, out)

	if errorOutput != nil {
		outputs = append(outputs, &errorLevelWriter{zerolog.ConsoleWriter{
			Out:        errorOutput,
			TimeFormat: RFC3339Milli,
			NoColor:    true, // error log files should not be colored
		}})
	}
	if panicOnInvalidChars {
		outputs = append(outputs, zerolog.ConsoleWriter{Out: &invalidCharChecker{}})
	}
	if len(outputs) > 1 {
		out = zerolog.MultiLevelWriter(outputs...)
	}

	// Init the global logger var, with millisecond timestamps
	logger := zerolog.New(out).With().Timestamp().Logger()
	if output == logTestWriterName {
		zerolog.TimestampFunc = func() time.Time { return logTestTime }
	}
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs

	// Include caller, increasing SkipFrameCount to account for this log package wrapper
	logger = logger.With().Caller().Logger()
	zerolog.CallerSkipFrameCount = 3
	zerolog.CallerMarshalFunc = func(pc uintptr, file string, line int) string {
		return fmt.Sprintf("%s/%s:%d", path.Base(path.Dir(file)), path.Base(file), line)
	}

	switch level {
	case LogLevelDebug:
		logger = logger.Level(zerolog.DebugLevel)
	case LogLevelInfo:
		logger = logger.Level(zerolog.InfoLevel)
	case LogLevelWarn:
		logger = logger.Level(zerolog.WarnLevel)
	case LogLevelError:
		logger = logger.Level(zerolog.ErrorLevel)
	default:
		panic(fmt.Sprintf("invalid log level: %q", level))
	}

	setLogger(logger)
	logger.Info().Msgf("logger construction succeeded at level %s with output %s", level, output)
}

// Level returns the current log level
func Level() string {
	logger := getLogger()
	switch level := logger.GetLevel(); level {
	case zerolog.DebugLevel:
		return LogLevelDebug
	case zerolog.InfoLevel:
		return LogLevelInfo
	case zerolog.WarnLevel:
		return LogLevelWarn
	case zerolog.ErrorLevel:
		return LogLevelError
	default:
		panic(fmt.Sprintf("invalid log level: %q", level))
	}
}

// Debug sends a debug level log message
func Debug(args ...any) {
	logger := getLogger()
	if logger.GetLevel() > zerolog.DebugLevel {
		return
	}
	logger.Debug().Msg(fmt.Sprint(args...))
}

// Info sends an info level log message
func Info(args ...any) {
	logger := getLogger()
	logger.Info().Msg(fmt.Sprint(args...))
}

// Monitor is a wrapper around Info that allows passing a map of key-value pairs.
// This is useful for structured logging and monitoring.
// The caller information is skipped.
func Monitor(msg string, args map[string]any) {
	logger := getLogger()
	logger.Info().CallerSkipFrame(100).Fields(args).Msg(msg)
}

// Warn sends a warn level log message
func Warn(args ...any) {
	logger := getLogger()
	logger.Warn().Msg(fmt.Sprint(args...))
}

// Error sends an error level log message
func Error(args ...any) {
	logger := getLogger()
	logger.Error().Msg(fmt.Sprint(args...))
}

// Fatal sends a fatal level log message
func Fatal(args ...any) {
	logger := getLogger()
	logger.Fatal().Msg(fmt.Sprint(args...) + "\n" + string(debug.Stack()))
	// We don't support log levels lower than "fatal". Help analyzers like
	// staticcheck see that, in this package, Fatal will always exit the
	// entire program.
	panic("unreachable")
}

// Debugf sends a formatted debug level log message
func Debugf(template string, args ...any) {
	Logger().Debug().Msgf(template, args...)
}

// Infof sends a formatted info level log message
func Infof(template string, args ...any) {
	Logger().Info().Msgf(template, args...)
}

// Warnf sends a formatted warn level log message
func Warnf(template string, args ...any) {
	Logger().Warn().Msgf(template, args...)
}

// Errorf sends a formatted error level log message
func Errorf(template string, args ...any) {
	Logger().Error().Msgf(template, args...)
}

// Fatalf sends a formatted fatal level log message
func Fatalf(template string, args ...any) {
	Logger().Fatal().Msgf(template+"\n"+string(debug.Stack()), args...)
}

// Debugw sends a debug level log message with key-value pairs.
func Debugw(msg string, keyvalues ...any) {
	Logger().Debug().Fields(keyvalues).Msg(msg)
}

// Infow sends an info level log message with key-value pairs.
func Infow(msg string, keyvalues ...any) {
	Logger().Info().Fields(keyvalues).Msg(msg)
}

// Warnw sends a warning level log message with key-value pairs.
func Warnw(msg string, keyvalues ...any) {
	Logger().Warn().Fields(keyvalues).Msg(msg)
}

// Errorw sends an error level log message with a special format for errors.
func Errorw(err error, msg string) {
	Logger().Error().Err(err).Msg(msg)
}
