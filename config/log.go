package config

import (
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func InitLogger(c *Config) {
	writeSyncer := getLogWriter(c)
	encoder := getEncoder()
	core := zapcore.NewCore(encoder, writeSyncer, zapcore.DebugLevel)
	logger := zap.New(core, zap.AddStacktrace(zapcore.WarnLevel), zap.AddCaller())
	zap.ReplaceGlobals(logger)
}

// getEncoder creates and returns a configured encoder for the logger.
func getEncoder() zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.TimeKey = "time"
	encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	encoderConfig.EncodeDuration = zapcore.SecondsDurationEncoder
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	return zapcore.NewConsoleEncoder(encoderConfig)
}

// getLogWriter returns a WriteSyncer that writes logs to os.Stdout.
func getLogWriter(c *Config) zapcore.WriteSyncer {
	return zapcore.AddSync(NewMultiWrite(c))
}

func NewMultiWrite(c *Config) io.Writer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   c.Logger.Path + c.Logger.FileName,
		MaxSize:    c.Logger.MaxSize,
		MaxAge:     c.Logger.MaxAge,
		MaxBackups: c.Logger.MaxBackups,
		Compress:   false,
	}

	syncFile := zapcore.AddSync(lumberJackLogger)
	syncConsole := zapcore.AddSync(os.Stdout)
	return io.MultiWriter(syncFile, syncConsole)
}
