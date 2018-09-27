package log

type (
	Logger interface {
		Debugf(format string, args ...interface{})
		Infof(format string, args ...interface{})
		Warnf(format string, args ...interface{})
		Errorf(format string, args ...interface{})

		Debug(args ...interface{})
		Info(args ...interface{})
		Warn(args ...interface{})
		Error(args ...interface{})
	}

	noOpLogger struct{}
)

var l Logger = &noOpLogger{}

func SetLogger(logger Logger) {
	l = logger
}

func (l *noOpLogger) Debugf(format string, args ...interface{}) {}
func (l *noOpLogger) Infof(format string, args ...interface{})  {}
func (l *noOpLogger) Warnf(format string, args ...interface{})  {}
func (l *noOpLogger) Errorf(format string, args ...interface{}) {}

func (l *noOpLogger) Debug(args ...interface{}) {}
func (l *noOpLogger) Info(args ...interface{})  {}
func (l *noOpLogger) Warn(args ...interface{})  {}
func (l *noOpLogger) Error(args ...interface{}) {}

func Debugf(format string, args ...interface{}) {
	l.Debugf(format, args...)
}
func Infof(format string, args ...interface{}) {
	l.Infof(format, args...)
}
func Warnf(format string, args ...interface{}) {
	l.Warnf(format, args...)
}
func Errorf(format string, args ...interface{}) {
	l.Errorf(format, args...)
}

func Debug(args ...interface{}) {
	l.Debug(args...)
}
func Info(args ...interface{}) {
	l.Info(args...)
}
func Warn(args ...interface{}) {
	l.Warn(args...)
}
func Error(args ...interface{}) {
	l.Error(args...)
}
