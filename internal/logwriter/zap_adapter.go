// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package logwriter

import (
	"github.com/rs/zerolog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// levelMap maps a zapcore.Level to the corresponding zerolog.Level.
var levelMap = map[zapcore.Level]zerolog.Level{
	zapcore.DebugLevel:  zerolog.DebugLevel,
	zapcore.InfoLevel:   zerolog.InfoLevel,
	zapcore.WarnLevel:   zerolog.WarnLevel,
	zapcore.ErrorLevel:  zerolog.ErrorLevel,
	zapcore.DPanicLevel: zerolog.PanicLevel,
	zapcore.PanicLevel:  zerolog.PanicLevel,
	zapcore.FatalLevel:  zerolog.FatalLevel,
}

// ZapToZerologAdapter is a zapcore.Core that forwards messages to a
// zerolog.Logger.
type ZapToZerologAdapter struct {
	Logger *zerolog.Logger
}

func (a *ZapToZerologAdapter) Enabled(level zapcore.Level) bool {
	zlLevel := levelMap[level]
	return zlLevel >= a.Logger.GetLevel()
}

func (a *ZapToZerologAdapter) With(fields []zapcore.Field) zapcore.Core {
	clone := ZapToZerologAdapter{}
	return &clone
}

func (a *ZapToZerologAdapter) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if a.Enabled(e.Level) {
		return ce.AddCore(e, a)
	}

	return ce
}

func (a *ZapToZerologAdapter) Write(e zapcore.Entry, fields []zapcore.Field) error {
	zlLevel := levelMap[e.Level]
	zlFields := make(map[string]interface{})
	for _, field := range fields {
		zlFields[field.Key] = field.Interface
	}
	a.Logger.WithLevel(zlLevel).Fields(zlFields).Msg(e.Message)
	return nil
}

func (a *ZapToZerologAdapter) Sync() error {
	// TODO: do we need to implement this?
	return nil
}

func (a *ZapToZerologAdapter) ZapLogger() *zap.Logger {
	return zap.New(a)
}
