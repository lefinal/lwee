package logging

import (
	"fmt"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehhttp"
	"github.com/lefinal/meh/mehlog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"sync"
)

func init() {
	mehlog.OmitErrorMessageField(true)
}

// NewLogger creates a new zap.Logger. Don't forget to call Sync() on the
// returned logged before exiting!
func NewLogger(level zapcore.Level) (*zap.Logger, error) {
	config := zap.NewProductionConfig()
	config.Encoding = "console"
	config.EncoderConfig = zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.RFC3339TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	config.OutputPaths = []string{"stdout"}
	config.Level = zap.NewAtomicLevelAt(level)
	config.DisableCaller = true
	config.DisableStacktrace = true
	logger, err := config.Build()
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "new zap production logger", meh.Details{"config": config})
	}
	mehlog.SetDefaultLevelTranslator(func(_ meh.Code) zapcore.Level {
		return zapcore.ErrorLevel
	})
	return logger, nil
}

var (
	logger      *zap.Logger
	loggerMutex sync.RWMutex
)

var defaultLevelTranslator map[meh.Code]zapcore.Level
var defaultLevelTranslatorMutex sync.RWMutex

func init() {
	defaultLevelTranslator = make(map[meh.Code]zapcore.Level)
	AddToDefaultLevelTranslator(meh.ErrNotFound, zap.DebugLevel)
	AddToDefaultLevelTranslator(meh.ErrUnauthorized, zap.DebugLevel)
	AddToDefaultLevelTranslator(meh.ErrForbidden, zap.DebugLevel)
	AddToDefaultLevelTranslator(meh.ErrBadInput, zap.DebugLevel)
	AddToDefaultLevelTranslator(mehhttp.ErrCommunication, zap.DebugLevel)
	mehlog.SetDefaultLevelTranslator(func(code meh.Code) zapcore.Level {
		defaultLevelTranslatorMutex.RLock()
		defer defaultLevelTranslatorMutex.RUnlock()
		if level, ok := defaultLevelTranslator[code]; ok {
			return level
		}
		return zap.ErrorLevel
	})
}

// AddToDefaultLevelTranslator adds the given case to the translation map.
func AddToDefaultLevelTranslator(code meh.Code, level zapcore.Level) {
	defaultLevelTranslatorMutex.Lock()
	defaultLevelTranslator[code] = level
	defaultLevelTranslatorMutex.Unlock()
}

// DebugLogger returns the debug logger from SetLogger. If none is set, a new one
// will be created.
func DebugLogger() *zap.Logger {
	return RootLogger().Named("debug")
}

// RootLogger returns the logger set via SetLogger. If none is set, a new one
// will be created.
func RootLogger() *zap.Logger {
	loggerMutex.RLock()
	defer loggerMutex.RUnlock()
	if logger == nil {
		logger, _ = NewLogger(zap.InfoLevel)
	}
	return logger
}

// SetLogger sets the logger that is used for reporting errors in main as well as
// with DebugLogger.
func SetLogger(newLogger *zap.Logger) {
	loggerMutex.Lock()
	defer loggerMutex.Unlock()
	logger = newLogger
}

func WrapName(name string) string {
	return fmt.Sprintf("<%s>", name)
}

// FormatByteCountDecimal formats bytes, e.g., 1024, as decimal with units, e.g.,
// 1kB.
//
// Taken from
// https://programming.guide/go/formatting-byte-size-to-human-readable-format.html.
func FormatByteCountDecimal(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}
