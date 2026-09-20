// Package queue holds kyu's domain vocabulary: what a queue's policy is, what
// a lease names, what a queue may be called, and what order its messages are
// in.
//
// Nothing here performs I/O. There is no gRPC client, no connection and no
// clock — every type is a value, and every function is a total function of its
// arguments. That is deliberate: these are the rules the engine is made of,
// and rules that need a server to exercise them are rules nobody checks.
//
// The vocabulary stops at this package. Storage never sees any of it: a
// queue's policy reaches storage as opaque bytes, and a lease reaches the
// consumer as an opaque token.
package queue

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// Default queue settings, applied when a queue is created without them.
const (
	DefaultHoldDuration   = 30 * time.Second
	DefaultAttemptCeiling = 5
)

// SettingsVersion is the first byte of every encoded settings value.
//
// It exists so that an engine meeting a value a later engine wrote can say so
// rather than misread it. A value this engine cannot decode is not guessed at:
// see DecodeSettings and the refusal it feeds.
const SettingsVersion byte = 1

// SettingsSize is the exact length of a version 1 value: one version byte, the
// hold duration as nanoseconds, and the attempt ceiling.
const SettingsSize = 1 + 8 + 4

// ErrUndecodableSettings is what every settings decode failure reduces to. A
// caller distinguishes decodable from not; why it was not is for the report.
var ErrUndecodableSettings = errors.New("settings cannot be decoded by this engine")

// Settings is a queue's durable policy.
//
// It reaches storage as the opaque bytes of kyu.storage.v1.Partition.settings,
// which storage stores and returns without reading. That is what lets
// per-queue policy be durable while no field of the SPI names a hold or a
// ceiling.
type Settings struct {
	HoldDuration   time.Duration
	AttemptCeiling uint32
}

// DefaultSettings is what a queue created without settings is recorded with.
//
// The defaults are encoded at creation rather than assumed at read time, so
// that what ListPartitions returns is the whole of a queue's policy.
func DefaultSettings() Settings {
	return Settings{
		HoldDuration:   DefaultHoldDuration,
		AttemptCeiling: DefaultAttemptCeiling,
	}
}

// WithDefaults fills in whichever of the two the caller left unset.
func (s Settings) WithDefaults() Settings {
	if s.HoldDuration <= 0 {
		s.HoldDuration = DefaultHoldDuration
	}
	if s.AttemptCeiling == 0 {
		s.AttemptCeiling = DefaultAttemptCeiling
	}
	return s
}

// Encode renders the settings as the opaque value storage records.
func (s Settings) Encode() []byte {
	b := make([]byte, SettingsSize)
	b[0] = SettingsVersion
	binary.BigEndian.PutUint64(b[1:9], uint64(s.HoldDuration.Nanoseconds()))
	binary.BigEndian.PutUint32(b[9:13], s.AttemptCeiling)
	return b
}

// DecodeSettings reads back what Encode wrote.
//
// It never falls back to defaults. The reason the ceiling is stored at all is
// that guessing it risks delivering a message past a limit the engine cannot
// see, so a value that will not decode is an error and the queue it belongs to
// is not served.
func DecodeSettings(b []byte) (Settings, error) {
	if len(b) != SettingsSize {
		return Settings{}, fmt.Errorf("%w: expected %d bytes, got %d", ErrUndecodableSettings, SettingsSize, len(b))
	}
	if b[0] != SettingsVersion {
		return Settings{}, fmt.Errorf("%w: version %d, this engine reads version %d", ErrUndecodableSettings, b[0], SettingsVersion)
	}
	s := Settings{
		HoldDuration:   time.Duration(int64(binary.BigEndian.Uint64(b[1:9]))),
		AttemptCeiling: binary.BigEndian.Uint32(b[9:13]),
	}
	if s.HoldDuration <= 0 {
		return Settings{}, fmt.Errorf("%w: hold duration %s is not positive", ErrUndecodableSettings, s.HoldDuration)
	}
	if s.AttemptCeiling == 0 {
		return Settings{}, fmt.Errorf("%w: attempt ceiling is zero, which would deliver nothing", ErrUndecodableSettings)
	}
	return s, nil
}
