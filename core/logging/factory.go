package logging

import (
	"context"
	"io"
	"os"
	"sync"
	"sync/atomic"

	configlogging "github.com/MalenkiySolovey/solovey-ui/config/logging"
	suiLog "github.com/MalenkiySolovey/solovey-ui/logger"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/observable"
	"github.com/sagernet/sing/service/filemanager"
)

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
	if options.PlatformWriter != nil {
		factory.AttachPlatformWriter(options.PlatformWriter)
	}
	if logOptions.Level != "" {
		logLevel, err := log.ParseLevel(logOptions.Level)
		if err != nil {
			_ = factory.Close()
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
	access         sync.Mutex
	ctx            context.Context
	formatter      log.Formatter
	writer         io.Writer
	file           *os.File
	filePath       string
	level          atomic.Int32
	observer       *observable.Observer[log.Entry]
	platformWriter log.PlatformWriter
	closed         bool
	started        bool
	closeOnce      sync.Once
	closeErr       error
	secrets        []string
}

func NewDefaultFactory(
	ctx context.Context,
	formatter log.Formatter,
	writer io.Writer,
	filePath string,
) log.ObservableFactory {
	if ctx == nil {
		ctx = context.Background()
	}
	subscriber := observable.NewSubscriber[log.Entry](128)
	factory := &defaultFactory{
		ctx:       ctx,
		formatter: formatter,
		writer:    writer,
		filePath:  filePath,
		observer:  observable.NewObserver(subscriber, 128),
	}
	factory.SetLevel(log.LevelTrace)
	if secrets, ok := ctx.Value(secretContextKey{}).([]string); ok {
		factory.secrets = append([]string(nil), secrets...)
	}
	return factory
}

func (f *defaultFactory) Start() error {
	f.access.Lock()
	defer f.access.Unlock()
	if f.closed {
		return os.ErrClosed
	}
	if f.started {
		return nil
	}
	if f.filePath != "" {
		logFile, err := filemanager.OpenFile(f.ctx, f.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		f.writer = logFile
		f.file = logFile
	}
	f.started = true
	return nil
}

func (f *defaultFactory) Close() error {
	f.closeOnce.Do(func() {
		f.access.Lock()
		f.closed = true
		f.platformWriter = nil
		f.secrets = nil
		file := f.file
		f.file = nil
		f.access.Unlock()
		f.closeErr = common.Close(common.PtrOrNil(file), f.observer)
	})
	return f.closeErr
}

// A generation owns one attachment slot. Replacing it cannot accumulate writers;
// Close fences dispatch and releases the reference before subscriber teardown.
func (f *defaultFactory) AttachPlatformWriter(writer log.PlatformWriter) {
	f.access.Lock()
	defer f.access.Unlock()
	if !f.closed {
		f.platformWriter = writer
	}
}

func (f *defaultFactory) Level() log.Level {
	return log.Level(f.level.Load())
}

func (f *defaultFactory) SetLevel(level log.Level) {
	f.level.Store(int32(level))
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
