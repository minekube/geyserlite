// SPDX-License-Identifier: MIT
package geyserlite

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"go.minekube.com/connect/geyserliteabi"
)

var errCallbackNullPointer = errors.New("geyserlite: native verified ingress callback passed a null pointer")

const embeddedAssignmentTransportFailure int32 = -2147483648

type callbackRoots struct {
	current atomic.Pointer[callbackGeneration]

	openFn      func(uint64)
	verifiedFn  func(uintptr, uintptr, uint32, uint64)
	openPtr     uintptr
	verifiedPtr uintptr
}

type callbackGeneration struct {
	id     uint64
	broker *ingressBroker

	mu        sync.Mutex
	accepting bool
	inFlight  sync.WaitGroup
	fatalOnce sync.Once
	fatalWake chan error
}

func newCallbackGeneration(id uint64, broker *ingressBroker) *callbackGeneration {
	return &callbackGeneration{id: id, broker: broker, accepting: true, fatalWake: make(chan error, 1)}
}

func (g *callbackGeneration) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.accepting {
		return false
	}
	g.inFlight.Add(1)
	return true
}

func (g *callbackGeneration) closeGate() {
	g.mu.Lock()
	g.accepting = false
	g.mu.Unlock()
}

func (g *callbackGeneration) closeAndWait() {
	g.closeGate()
	g.inFlight.Wait()
}

func (g *callbackGeneration) fail(err error) {
	g.fatalOnce.Do(func() {
		g.closeGate()
		g.fatalWake <- err
		close(g.fatalWake)
	})
}

func newCallbackRoots(newPointer func(any) uintptr) *callbackRoots {
	r := &callbackRoots{}
	r.openFn = func(handle uint64) {
		g := r.current.Load()
		if g == nil || !g.enter() {
			return
		}
		defer g.inFlight.Done()
		if err := g.broker.open(g.id, handle); err != nil {
			g.fail(err)
		}
	}
	r.verifiedFn = func(correlationPtr, framePtr uintptr, frameLen uint32, expiresUnixMS uint64) {
		g := r.current.Load()
		if g == nil || !g.enter() {
			return
		}
		defer g.inFlight.Done()
		if correlationPtr == 0 || framePtr == 0 {
			g.fail(errCallbackNullPointer)
			return
		}
		if frameLen < geyserliteabi.MinIngressFrameBytes || frameLen > geyserliteabi.MaxIngressFrameBytes {
			g.fail(ErrIngressFrame)
			return
		}
		correlationBytes := copyNativeBytes(correlationPtr, geyserliteabi.CorrelationBytes)
		var correlation [geyserliteabi.CorrelationBytes]byte
		copy(correlation[:], correlationBytes)
		frame := copyNativeBytes(framePtr, int(frameLen))
		if err := g.broker.deliverCallback(g.id, correlation, frame, expiresUnixMS); err != nil {
			g.fail(err)
		}
	}
	r.openPtr = newPointer(r.openFn)
	r.verifiedPtr = newPointer(r.verifiedFn)
	return r
}

//go:nocheckptr
func copyNativeBytes(pointer uintptr, length int) []byte {
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(pointer)), length)...)
}

var processCallbackRoots = sync.OnceValue(func() *callbackRoots {
	return newCallbackRoots(purego.NewCallback)
})

var embeddedGenerationCounter atomic.Uint64

func nextEmbeddedGeneration() uint64 {
	return embeddedGenerationCounter.Add(1)
}

func (r *embeddedRunner) assignIngress(isolate uintptr, assignment ConnectionAssignment) int32 {
	rc := embeddedAssignmentTransportFailure
	err := r.callOnAttachedThread(isolate, func(thread uintptr) error {
		rc = r.api.assignVerifiedIngress(
			thread,
			uintptr(assignment.ConnectionHandle),
			ptrOf(assignment.CorrelationID[:]),
			uint64(assignment.ExpiresAt.UnixMilli()),
		)
		runtime.KeepAlive(assignment.CorrelationID)
		return nil
	})
	if err != nil {
		return embeddedAssignmentTransportFailure
	}
	return rc
}

func (r *embeddedRunner) startIngressGeneration(isolate, thread uintptr, broker *ingressBroker, roots *callbackRoots, generation *callbackGeneration) error {
	broker.activate(generation.id, func(assignment ConnectionAssignment) int32 {
		if !generation.enter() {
			return embeddedAssignmentTransportFailure
		}
		defer generation.inFlight.Done()
		return r.assignIngress(isolate, assignment)
	}, generation.fail)
	if !roots.current.CompareAndSwap(nil, generation) {
		generation.closeAndWait()
		broker.deactivate(generation.id)
		return fmt.Errorf("%w: another embedded callback generation is active", ErrIngressABI)
	}
	if rc := r.api.setIngressCallbacks(thread, roots.openPtr, roots.verifiedPtr); rc != geyserliteabi.CallbackRegistrationOK {
		// Drain through the sole frozen unregister operation in case native
		// code observed either pointer before reporting registration failure.
		r.api.setIngressCallbacks(thread, 0, 0)
		generation.closeAndWait()
		broker.deactivate(generation.id)
		clearCurrentGeneration(roots, generation)
		return fmt.Errorf("%w: callback registration rc=%d", ErrIngressABI, rc)
	}
	return nil
}

func (r *embeddedRunner) stopIngressGeneration(isolate uintptr, broker *ingressBroker, roots *callbackRoots, generation *callbackGeneration) error {
	generation.closeGate()
	broker.deactivate(generation.id)
	err := r.callOnAttachedThread(isolate, func(thread uintptr) error {
		if rc := r.api.setIngressCallbacks(thread, 0, 0); rc != geyserliteabi.CallbackRegistrationOK {
			return fmt.Errorf("%w: callback unregister rc=%d", ErrIngressABI, rc)
		}
		return nil
	})
	generation.inFlight.Wait()
	clearCurrentGeneration(roots, generation)
	return err
}

func clearCurrentGeneration(roots *callbackRoots, generation *callbackGeneration) {
	roots.current.CompareAndSwap(generation, nil)
	runtime.KeepAlive(roots)
}
