package logging

import (
	"context"
	"io"
	"os"

	configlogging "github.com/MalenkiySolovey/solovey-ui/config/logging"
	suiLog "github.com/MalenkiySolovey/solovey-ui/logger"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/observable"
	"github.com/sagernet/sing/service/filemanager"
)

type PlatformWriter struct{}

func (p PlatformWriter) DisableColors() bool {
	return true
}
func (p PlatformWriter) WriteMessage(level log.Level, message string) {
	switch level {
	case log.LevelInfo:
		suiLog.CoreInfo(message)
	case log.LevelWarn:
		suiLog.CoreWarning(message)
	case log.LevelPanic:
	case log.LevelFatal:
	case log.LevelError:
		suiLog.CoreError(message)
	default:
		suiLog.CoreDebug(message)
	}
}

func NewFactory(options log.Options) (log.Factory, error) {
	logOptions := options.Options

	if logOptions.Disabled {
		return log.NewNOPFactory(), nil
	}

	var logWriter io.Writer
	var logFilePath string

	switch logOptions.Output {
	case "":
		logWriter = options.DefaultWriter
		if logWriter == nil {
			logWriter = os.Stderr
		}
	case "stderr":
		logWriter = os.Stderr
	case "stdout":
		logWriter = os.Stdout
	default:
		if !configlogging.IsSafeLogOutputPath(logOptions.Output) {
			suiLog.CoreWarning("ignoring unsafe log.output path; writing to stderr instead: ", logOptions.Output)
			logWriter = os.Stderr
		} else {
			logFilePath = logOptions.Output
		}
	}
	logFormatter := log.Formatter{
		BaseTime:         options.BaseTime,
		DisableColors:    logOptions.DisableColor || logFilePath != "",
		DisableTimestamp: !logOptions.Timestamp && logFilePath != "",
		FullTimestamp:    logOptions.Timestamp,
		TimestampFormat:  "-0700 2006-01-02 15:04:05",
	}
	factory := NewDefaultFactory(
		options.Context,
		logFormatter,
		logWriter,
		logFilePath,
	)
	if logOptions.Level != "" {
		logLevel, err := log.ParseLevel(logOptions.Level)
		if err != nil {
			return nil, common.Error("parse log level", err)
		}
		factory.SetLevel(logLevel)
	} else {
		factory.SetLevel(log.LevelTrace)
	}
	return factory, nil
}

var _ log.Factory = (*defaultFactory)(nil)

type defaultFactory struct {
	ctx       context.Context
	formatter log.Formatter
	writer    io.Writer
	file      *os.File
	filePath  string
	level     log.Level
	observer  *observable.Observer[log.Entry]
}

func NewDefaultFactory(
	ctx context.Context,
	formatter log.Formatter,
	writer io.Writer,
	filePath string,
) log.ObservableFactory {
	subscriber := observable.NewSubscriber[log.Entry](128)
	factory := &defaultFactory{
		ctx:       ctx,
		formatter: formatter,
		writer:    writer,
		filePath:  filePath,
		level:     log.LevelTrace,
		observer:  observable.NewObserver(subscriber, 128),
	}
	return factory
}

func (f *defaultFactory) Start() error {
	if f.filePath != "" {
		logFile, err := filemanager.OpenFile(f.ctx, f.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		f.writer = logFile
		f.file = logFile
	}
	return nil
}

func (f *defaultFactory) Close() error {
	return common.Close(
		common.PtrOrNil(f.file),
		f.observer,
	)
}

func (f *defaultFactory) Level() log.Level {
	return f.level
}

func (f *defaultFactory) SetLevel(level log.Level) {
	f.level = level
}

func (f *defaultFactory) Logger() log.ContextLogger {
	return f.NewLogger("")
}

func (f *defaultFactory) NewLogger(tag string) log.ContextLogger {
	return &observableLogger{f, tag}
}

func (f *defaultFactory) Subscribe() (subscription observable.Subscription[log.Entry], done <-chan struct{}, err error) {
	return f.observer.Subscribe()
}

func (f *defaultFactory) UnSubscribe(sub observable.Subscription[log.Entry]) {
	f.observer.UnSubscribe(sub)
}
