// SPDX-License-Identifier: MIT
package geyserlite

import (
	"errors"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"go.minekube.com/connect/geyserliteabi"
)

func TestEmbeddedIngressCallbackCopiesAndCorrelates(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := newIngressBroker(2, func() time.Time { return now })
	g := newCallbackGeneration(11, b)
	b.activate(11, func(ConnectionAssignment) int32 { return geyserliteabi.AssignmentOK }, g.fail)
	roots := newCallbackRoots(func(any) uintptr { return 1 })
	roots.current.Store(g)

	roots.openFn(72)
	opened := <-b.opened
	if opened.ConnectionHandle != 72 {
		t.Fatalf("open handle = %d", opened.ConnectionHandle)
	}
	corr := [16]byte{4, 5, 6}
	expires := now.Add(time.Second)
	if err := b.assign(ConnectionAssignment{Generation: 11, ConnectionHandle: 72, CorrelationID: corr, ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	frame := []byte{8, 1}
	roots.verifiedFn(uintptr(unsafe.Pointer(&corr[0])), uintptr(unsafe.Pointer(&frame[0])), uint32(len(frame)), uint64(expires.UnixMilli()))
	frame[0] = 0xff
	runtime.KeepAlive(corr)

	got := <-b.verified
	if got.Frame()[0] != 8 || got.CorrelationID() != corr {
		t.Fatalf("verified callback was not synchronously copied/correlated: %x %x", got.Frame(), got.CorrelationID())
	}
	select {
	case err := <-g.fatalWake:
		t.Fatalf("valid callback became fatal: %v", err)
	default:
	}
}

func TestEmbeddedIngressCallbackRejectsPointersAndBoundsBeforeRead(t *testing.T) {
	tests := []struct {
		name        string
		correlation uintptr
		frame       uintptr
		length      uint32
		want        error
	}{
		{name: "null-correlation", correlation: 0, frame: 1, length: 1, want: errCallbackNullPointer},
		{name: "null-frame", correlation: 1, frame: 0, length: 1, want: errCallbackNullPointer},
		{name: "zero", correlation: 1, frame: 1, length: 0, want: ErrIngressFrame},
		{name: "4097", correlation: 1, frame: 1, length: 4097, want: ErrIngressFrame},
		{name: "max-uint32", correlation: 1, frame: 1, length: ^uint32(0), want: ErrIngressFrame},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := newIngressBroker(1, time.Now)
			g := newCallbackGeneration(1, b)
			b.activate(1, func(ConnectionAssignment) int32 { return 0 }, g.fail)
			roots := newCallbackRoots(func(any) uintptr { return 1 })
			roots.current.Store(g)
			roots.verifiedFn(tc.correlation, tc.frame, tc.length, uint64(time.Now().Add(time.Second).UnixMilli()))
			if err := <-g.fatalWake; !errors.Is(err, tc.want) {
				t.Fatalf("fatal = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCallbackGenerationCloseWaitsForEnteredCallback(t *testing.T) {
	g := newCallbackGeneration(1, newIngressBroker(1, time.Now))
	if !g.enter() {
		t.Fatal("generation did not accept callback")
	}
	done := make(chan struct{})
	go func() {
		g.closeAndWait()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("close returned while callback was in flight")
	case <-time.After(20 * time.Millisecond):
	}
	g.inFlight.Done()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close did not return after callback exited")
	}
	if g.enter() {
		t.Fatal("closed generation accepted callback")
	}
}

func TestEmbeddedIngressAssignmentUsesAttachedThreadAndFrozenReturnCodes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	corr := [16]byte{1}
	a := ConnectionAssignment{Generation: 9, ConnectionHandle: 3, CorrelationID: corr, ExpiresAt: now.Add(time.Second)}

	var gotThread, gotHandle uintptr
	var gotExpires uint64
	r := &embeddedRunner{api: geyserAPI{
		attachThread: func(isolate uintptr, thread *uintptr) int32 { *thread = 22; return 0 },
		detachThread: func(thread uintptr) int32 { return 0 },
		assignVerifiedIngress: func(thread, handle, correlation uintptr, expires uint64) int32 {
			gotThread, gotHandle, gotExpires = thread, handle, expires
			if copyNativeBytes(correlation, 1)[0] != 1 {
				t.Fatal("correlation pointer did not contain assignment bytes")
			}
			return geyserliteabi.AssignmentDuplicateHandleOrCorrelation
		},
	}}
	rc := r.assignIngress(44, a)
	if rc != geyserliteabi.AssignmentDuplicateHandleOrCorrelation || gotThread != 22 || gotHandle != 3 || gotExpires != uint64(a.ExpiresAt.UnixMilli()) {
		t.Fatalf("assignment result rc=%d thread=%d handle=%d expires=%d", rc, gotThread, gotHandle, gotExpires)
	}
}

func TestEmbeddedIngressRegistrationAndUnregisterBarrier(t *testing.T) {
	b := newIngressBroker(1, time.Now)
	g := newCallbackGeneration(8, b)
	roots := newCallbackRoots(func(fn any) uintptr {
		switch fn.(type) {
		case func(uint64):
			return 101
		default:
			return 202
		}
	})
	var calls [][3]uintptr
	r := &embeddedRunner{api: geyserAPI{
		attachThread: func(isolate uintptr, thread *uintptr) int32 { *thread = 77; return 0 },
		detachThread: func(thread uintptr) int32 { return 0 },
		setIngressCallbacks: func(thread, open, verified uintptr) int32 {
			calls = append(calls, [3]uintptr{thread, open, verified})
			return geyserliteabi.CallbackRegistrationOK
		},
		assignVerifiedIngress: func(uintptr, uintptr, uintptr, uint64) int32 { return 0 },
	}}
	if err := r.startIngressGeneration(55, 66, b, roots, g); err != nil {
		t.Fatal(err)
	}
	if roots.current.Load() != g {
		t.Fatal("generation was not published")
	}
	if err := r.stopIngressGeneration(55, b, roots, g); err != nil {
		t.Fatal(err)
	}
	if roots.current.Load() != nil {
		t.Fatal("generation remained published after unregister")
	}
	want := [][3]uintptr{{66, 101, 202}, {77, 0, 0}}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("registration calls = %#v, want %#v", calls, want)
	}
}

func TestEmbeddedIngressRegistrationFailureIsFailClosed(t *testing.T) {
	b := newIngressBroker(1, time.Now)
	g := newCallbackGeneration(8, b)
	roots := newCallbackRoots(func(any) uintptr { return 1 })
	var calls [][2]uintptr
	r := &embeddedRunner{api: geyserAPI{
		setIngressCallbacks: func(_ uintptr, open, verified uintptr) int32 {
			calls = append(calls, [2]uintptr{open, verified})
			if open == 0 && verified == 0 {
				return geyserliteabi.CallbackRegistrationOK
			}
			return -9
		},
		assignVerifiedIngress: func(uintptr, uintptr, uintptr, uint64) int32 { return 0 },
	}}
	if err := r.startIngressGeneration(1, 2, b, roots, g); !errors.Is(err, ErrIngressABI) {
		t.Fatalf("registration error = %v, want ErrIngressABI", err)
	}
	if roots.current.Load() != nil {
		t.Fatal("failed registration left callback generation published")
	}
	if len(calls) != 2 || calls[1] != [2]uintptr{} {
		t.Fatalf("failed registration did not invoke null/null barrier: %#v", calls)
	}
	if err := b.open(8, 1); !errors.Is(err, ErrIngressClosed) {
		t.Fatalf("broker after failed registration = %v", err)
	}
}
