// SPDX-License-Identifier: MIT
package geyserlite

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"go.minekube.com/connect/geyserliteabi"
)

const defaultIngressQueueCapacity = 256

var (
	ErrIngressABI            = errors.New("geyserlite: verified ingress ABI mismatch")
	ErrIngressAuthentication = errors.New("geyserlite: verified ingress authentication failed")
	ErrIngressClosed         = errors.New("geyserlite: verified ingress connection is closed")
	ErrIngressDuplicate      = errors.New("geyserlite: duplicate verified ingress assignment")
	ErrIngressFrame          = errors.New("geyserlite: invalid verified ingress frame")
	ErrIngressGeneration     = errors.New("geyserlite: stale verified ingress generation")
	ErrIngressLifetime       = errors.New("geyserlite: invalid verified ingress lifetime")
	ErrIngressMismatch       = errors.New("geyserlite: verified ingress correlation mismatch")
	ErrIngressOverflow       = errors.New("geyserlite: verified ingress queue overflow")
	ErrIngressRejected       = errors.New("geyserlite: native verified ingress assignment rejected")
	ErrIngressSequence       = errors.New("geyserlite: verified ingress sequence mismatch")
)

// ConnectionOpen identifies one native Bedrock transport before Geyser creates
// or authenticates a session. The handle is opaque and process-local.
type ConnectionOpen struct {
	Generation       uint64
	ConnectionHandle uint64
}

// ConnectionAssignment binds a Gate-created correlation to one live native
// connection for at most the frozen five-second ingress lifetime.
type ConnectionAssignment struct {
	Generation       uint64
	ConnectionHandle uint64
	CorrelationID    [geyserliteabi.CorrelationBytes]byte
	ExpiresAt        time.Time
}

// VerifiedFrame is an opaque verifier-produced VerifiedIngressV1 protobuf.
// Its private marker prevents other packages from implementing the interface;
// GeyserLite does not parse or manufacture verified identity.
type VerifiedFrame interface {
	Generation() uint64
	ConnectionHandle() uint64
	CorrelationID() [geyserliteabi.CorrelationBytes]byte
	Frame() []byte
	ExpiresAt() time.Time
	verifiedIngressFrame()
}

type verifiedFrameValue struct {
	generation  uint64
	handle      uint64
	correlation [geyserliteabi.CorrelationBytes]byte
	frame       []byte
	expiresAt   time.Time
}

func (v verifiedFrameValue) Generation() uint64       { return v.generation }
func (v verifiedFrameValue) ConnectionHandle() uint64 { return v.handle }
func (v verifiedFrameValue) CorrelationID() [geyserliteabi.CorrelationBytes]byte {
	return v.correlation
}
func (v verifiedFrameValue) Frame() []byte        { return append([]byte(nil), v.frame...) }
func (v verifiedFrameValue) ExpiresAt() time.Time { return v.expiresAt }
func (verifiedFrameValue) verifiedIngressFrame()  {}

type ingressAssignFunc func(ConnectionAssignment) int32
type ingressFatalFunc func(error)

type ingressPending struct {
	handle      uint64
	correlation [geyserliteabi.CorrelationBytes]byte
	expiresAt   time.Time
	timer       *time.Timer
}

type ingressBroker struct {
	mu sync.Mutex

	now      func() time.Time
	opened   chan ConnectionOpen
	verified chan VerifiedFrame

	active       bool
	generation   uint64
	assignNative ingressAssignFunc
	fatal        ingressFatalFunc
	handles      map[uint64]struct{}
	correlations map[[geyserliteabi.CorrelationBytes]byte]ingressPending
}

func newIngressBroker(capacity int, now func() time.Time) *ingressBroker {
	if capacity < 1 {
		capacity = 1
	}
	if now == nil {
		now = time.Now
	}
	return &ingressBroker{
		now:          now,
		opened:       make(chan ConnectionOpen, capacity),
		verified:     make(chan VerifiedFrame, capacity),
		handles:      make(map[uint64]struct{}),
		correlations: make(map[[geyserliteabi.CorrelationBytes]byte]ingressPending),
	}
}

func (b *ingressBroker) activate(generation uint64, assign ingressAssignFunc, fatal ingressFatalFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active = generation != 0 && assign != nil
	b.generation = generation
	b.assignNative = assign
	b.fatal = fatal
	b.clearLocked()
}

func (b *ingressBroker) deactivate(generation uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.generation != generation {
		return
	}
	b.active = false
	b.assignNative = nil
	b.fatal = nil
	b.clearLocked()
}

func (b *ingressBroker) failLocked(err error) ingressFatalFunc {
	b.active = false
	b.assignNative = nil
	b.clearLocked()
	return b.fatal
}

func (b *ingressBroker) clearLocked() {
	for correlation, pending := range b.correlations {
		if pending.timer != nil {
			pending.timer.Stop()
		}
		delete(b.correlations, correlation)
	}
	clear(b.handles)
drainOpened:
	for {
		select {
		case <-b.opened:
		default:
			break drainOpened
		}
	}
drainVerified:
	for {
		select {
		case <-b.verified:
		default:
			break drainVerified
		}
	}
}

func (b *ingressBroker) failGeneration(generation uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if generation == b.generation {
		b.active = false
		b.assignNative = nil
		b.fatal = nil
		b.clearLocked()
	}
}

func (b *ingressBroker) open(generation, handle uint64) error {
	b.mu.Lock()
	if generation != b.generation {
		b.mu.Unlock()
		return ErrIngressGeneration
	}
	if !b.active {
		b.mu.Unlock()
		return ErrIngressClosed
	}
	if handle == 0 {
		fatal := b.failLocked(ErrIngressMismatch)
		b.mu.Unlock()
		if fatal != nil {
			fatal(ErrIngressMismatch)
		}
		return ErrIngressMismatch
	}
	if _, exists := b.handles[handle]; exists {
		fatal := b.failLocked(ErrIngressDuplicate)
		b.mu.Unlock()
		if fatal != nil {
			fatal(ErrIngressDuplicate)
		}
		return ErrIngressDuplicate
	}
	b.handles[handle] = struct{}{}
	select {
	case b.opened <- ConnectionOpen{Generation: generation, ConnectionHandle: handle}:
		b.mu.Unlock()
		return nil
	default:
		fatal := b.failLocked(ErrIngressOverflow)
		b.mu.Unlock()
		if fatal != nil {
			fatal(ErrIngressOverflow)
		}
		return ErrIngressOverflow
	}
}

func (b *ingressBroker) assign(a ConnectionAssignment) error {
	now := b.now()
	if !a.ExpiresAt.After(now) || a.ExpiresAt.Sub(now) > geyserliteabi.MaxIngressLifetime {
		b.closeHandle(a.Generation, a.ConnectionHandle)
		return ErrIngressLifetime
	}
	if a.CorrelationID == ([geyserliteabi.CorrelationBytes]byte{}) {
		b.closeHandle(a.Generation, a.ConnectionHandle)
		return ErrIngressMismatch
	}

	b.mu.Lock()
	if a.Generation != b.generation {
		b.mu.Unlock()
		return ErrIngressGeneration
	}
	if !b.active {
		b.mu.Unlock()
		return ErrIngressClosed
	}
	if _, ok := b.handles[a.ConnectionHandle]; !ok {
		b.mu.Unlock()
		return ErrIngressClosed
	}
	if _, exists := b.correlations[a.CorrelationID]; exists {
		b.clearAssignmentLocked(a.ConnectionHandle, a.CorrelationID)
		b.mu.Unlock()
		return ErrIngressDuplicate
	}
	for _, pending := range b.correlations {
		if pending.handle == a.ConnectionHandle {
			b.clearAssignmentLocked(a.ConnectionHandle, pending.correlation)
			b.mu.Unlock()
			return ErrIngressDuplicate
		}
	}
	pending := ingressPending{handle: a.ConnectionHandle, correlation: a.CorrelationID, expiresAt: a.ExpiresAt}
	pending.timer = time.AfterFunc(a.ExpiresAt.Sub(now), func() {
		b.expire(a.Generation, a.ConnectionHandle, a.CorrelationID, a.ExpiresAt)
	})
	b.correlations[a.CorrelationID] = pending
	assignNative := b.assignNative
	b.mu.Unlock()

	rc := assignNative(a)
	if rc == geyserliteabi.AssignmentOK {
		return nil
	}
	b.closeHandle(a.Generation, a.ConnectionHandle)
	switch rc {
	case geyserliteabi.AssignmentUnknownOrClosedHandle,
		geyserliteabi.AssignmentDuplicateHandleOrCorrelation,
		geyserliteabi.AssignmentInvalidOrExpiredTime,
		geyserliteabi.AssignmentWrongConnectionState:
		return fmt.Errorf("%w: rc=%d", ErrIngressRejected, rc)
	default:
		return fmt.Errorf("%w: assignment rc=%d", ErrIngressABI, rc)
	}
}

func (b *ingressBroker) closeHandle(generation, handle uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if generation != b.generation {
		return
	}
	delete(b.handles, handle)
	for correlation, pending := range b.correlations {
		if pending.handle == handle {
			if pending.timer != nil {
				pending.timer.Stop()
			}
			delete(b.correlations, correlation)
		}
	}
}

func (b *ingressBroker) clearAssignmentLocked(handle uint64, correlation [geyserliteabi.CorrelationBytes]byte) {
	delete(b.handles, handle)
	if pending, ok := b.correlations[correlation]; ok {
		if pending.timer != nil {
			pending.timer.Stop()
		}
		delete(b.correlations, correlation)
		delete(b.handles, pending.handle)
	}
	for otherCorrelation, pending := range b.correlations {
		if pending.handle != handle {
			continue
		}
		if pending.timer != nil {
			pending.timer.Stop()
		}
		delete(b.correlations, otherCorrelation)
	}
}

func (b *ingressBroker) expire(generation, handle uint64, correlation [geyserliteabi.CorrelationBytes]byte, expiresAt time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if generation != b.generation {
		return
	}
	pending, ok := b.correlations[correlation]
	if !ok || pending.handle != handle || !pending.expiresAt.Equal(expiresAt) {
		return
	}
	delete(b.correlations, correlation)
	delete(b.handles, handle)
}

func (b *ingressBroker) deliverCallback(generation uint64, correlation [geyserliteabi.CorrelationBytes]byte, frame []byte, expiresUnixMS uint64) error {
	return b.deliver(generation, correlation, frame, expiresUnixMS)
}

func (b *ingressBroker) deliverIPCFrame(generation, handle uint64, correlation [geyserliteabi.CorrelationBytes]byte, frame []byte) error {
	b.mu.Lock()
	pending, ok := b.correlations[correlation]
	b.mu.Unlock()
	if !ok || pending.handle != handle {
		return ErrIngressMismatch
	}
	return b.deliver(generation, correlation, frame, uint64(pending.expiresAt.UnixMilli()))
}

func (b *ingressBroker) deliver(generation uint64, correlation [geyserliteabi.CorrelationBytes]byte, frame []byte, expiresUnixMS uint64) error {
	if len(frame) < geyserliteabi.MinIngressFrameBytes || len(frame) > geyserliteabi.MaxIngressFrameBytes {
		b.closeCorrelation(generation, correlation)
		return ErrIngressFrame
	}
	expiresAt := time.UnixMilli(int64(expiresUnixMS))
	now := b.now()
	if !expiresAt.After(now) || expiresAt.Sub(now) > geyserliteabi.MaxIngressLifetime {
		b.closeCorrelation(generation, correlation)
		return ErrIngressLifetime
	}

	b.mu.Lock()
	if generation != b.generation {
		b.mu.Unlock()
		return ErrIngressGeneration
	}
	if !b.active {
		b.mu.Unlock()
		return ErrIngressClosed
	}
	pending, ok := b.correlations[correlation]
	if !ok {
		b.mu.Unlock()
		return ErrIngressMismatch
	}
	if pending.expiresAt.UnixMilli() != int64(expiresUnixMS) {
		if pending.timer != nil {
			pending.timer.Stop()
		}
		delete(b.correlations, correlation)
		delete(b.handles, pending.handle)
		b.mu.Unlock()
		return ErrIngressMismatch
	}
	if pending.timer != nil {
		pending.timer.Stop()
	}
	delete(b.correlations, correlation)
	delete(b.handles, pending.handle)
	value := verifiedFrameValue{generation: generation, handle: pending.handle, correlation: correlation, frame: append([]byte(nil), frame...), expiresAt: expiresAt}
	select {
	case b.verified <- value:
		b.mu.Unlock()
		return nil
	default:
		fatal := b.failLocked(ErrIngressOverflow)
		b.mu.Unlock()
		if fatal != nil {
			fatal(ErrIngressOverflow)
		}
		return ErrIngressOverflow
	}
}

func (b *ingressBroker) closeCorrelation(generation uint64, correlation [geyserliteabi.CorrelationBytes]byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if generation != b.generation {
		return
	}
	if pending, ok := b.correlations[correlation]; ok {
		if pending.timer != nil {
			pending.timer.Stop()
		}
		delete(b.handles, pending.handle)
		delete(b.correlations, correlation)
	}
}
