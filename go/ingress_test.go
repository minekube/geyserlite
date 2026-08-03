// SPDX-License-Identifier: MIT
package geyserlite

import (
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.minekube.com/connect/geyserliteabi"
)

func TestIngressBrokerCorrelatesCopiesAndConsumesExactlyOnce(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := newIngressBroker(4, func() time.Time { return now })
	b.activate(7, func(a ConnectionAssignment) int32 {
		if a.ConnectionHandle != 41 {
			t.Fatalf("assignment handle = %d", a.ConnectionHandle)
		}
		return geyserliteabi.AssignmentOK
	}, nil)

	if err := b.open(7, 41); err != nil {
		t.Fatal(err)
	}
	opened := <-b.opened
	if opened.Generation != 7 || opened.ConnectionHandle != 41 {
		t.Fatalf("opened = %+v", opened)
	}

	correlation := [geyserliteabi.CorrelationBytes]byte{1, 2, 3}
	expires := now.Add(geyserliteabi.MaxIngressLifetime)
	if err := b.assign(ConnectionAssignment{
		Generation:       7,
		ConnectionHandle: 41,
		CorrelationID:    correlation,
		ExpiresAt:        expires,
	}); err != nil {
		t.Fatal(err)
	}

	frame := []byte{0x08, 0x01}
	if err := b.deliverCallback(7, correlation, frame, uint64(expires.UnixMilli())); err != nil {
		t.Fatal(err)
	}
	frame[0] = 0xff

	got := <-b.verified
	if got.Generation() != 7 || got.ConnectionHandle() != 41 || got.CorrelationID() != correlation {
		t.Fatalf("verified metadata mismatch: generation=%d handle=%d correlation=%x", got.Generation(), got.ConnectionHandle(), got.CorrelationID())
	}
	if bytes := got.Frame(); len(bytes) != 2 || bytes[0] != 0x08 {
		t.Fatalf("frame was not synchronously copied: %x", bytes)
	}
	returned := got.Frame()
	returned[0] = 0xee
	if got.Frame()[0] != 0x08 {
		t.Fatal("Frame exposed mutable broker-owned memory")
	}
	if err := b.deliverCallback(7, correlation, []byte{1}, uint64(expires.UnixMilli())); !errors.Is(err, ErrIngressMismatch) {
		t.Fatalf("second delivery error = %v, want ErrIngressMismatch", err)
	}
}

func TestIngressBrokerRejectsLifetimeMismatchAndAssignmentFailure(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tests := []struct {
		name    string
		expires time.Time
		code    int32
		want    error
	}{
		{name: "expired", expires: now, code: geyserliteabi.AssignmentOK, want: ErrIngressLifetime},
		{name: "over-five-seconds", expires: now.Add(geyserliteabi.MaxIngressLifetime + time.Millisecond), code: geyserliteabi.AssignmentOK, want: ErrIngressLifetime},
		{name: "native-abi-mismatch", expires: now.Add(time.Second), code: 99, want: ErrIngressABI},
		{name: "native-rejection", expires: now.Add(time.Second), code: geyserliteabi.AssignmentWrongConnectionState, want: ErrIngressRejected},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := newIngressBroker(1, func() time.Time { return now })
			b.activate(1, func(ConnectionAssignment) int32 { return tc.code }, nil)
			if err := b.open(1, 1); err != nil {
				t.Fatal(err)
			}
			<-b.opened
			err := b.assign(ConnectionAssignment{Generation: 1, ConnectionHandle: 1, CorrelationID: [16]byte{1}, ExpiresAt: tc.expires})
			if !errors.Is(err, tc.want) {
				t.Fatalf("assign error = %v, want %v", err, tc.want)
			}
			if err := b.assign(ConnectionAssignment{Generation: 1, ConnectionHandle: 1, CorrelationID: [16]byte{2}, ExpiresAt: now.Add(time.Second)}); !errors.Is(err, ErrIngressClosed) {
				t.Fatalf("retry after failure = %v, want ErrIngressClosed", err)
			}
		})
	}
}

func TestIngressBrokerRejectsUnknownCorrelationExpiryAndGeneration(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := newIngressBroker(2, func() time.Time { return now })
	b.activate(3, func(ConnectionAssignment) int32 { return geyserliteabi.AssignmentOK }, nil)
	if err := b.open(3, 9); err != nil {
		t.Fatal(err)
	}
	<-b.opened
	corr := [16]byte{9}
	expires := now.Add(time.Second)
	if err := b.assign(ConnectionAssignment{Generation: 3, ConnectionHandle: 9, CorrelationID: corr, ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}

	if err := b.deliverCallback(2, corr, []byte{1}, uint64(expires.UnixMilli())); !errors.Is(err, ErrIngressGeneration) {
		t.Fatalf("old generation = %v", err)
	}
	if err := b.deliverCallback(3, [16]byte{8}, []byte{1}, uint64(expires.UnixMilli())); !errors.Is(err, ErrIngressMismatch) {
		t.Fatalf("unknown correlation = %v", err)
	}
	now = expires
	if err := b.deliverCallback(3, corr, []byte{1}, uint64(expires.UnixMilli())); !errors.Is(err, ErrIngressLifetime) {
		t.Fatalf("expired callback = %v", err)
	}
	select {
	case <-b.verified:
		t.Fatal("rejected callback published a verified frame")
	default:
	}
}

func TestIngressBrokerOverflowFailsClosedAndCleansGeneration(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	fatal := make(chan error, 1)
	b := newIngressBroker(1, func() time.Time { return now })
	b.activate(5, func(ConnectionAssignment) int32 { return geyserliteabi.AssignmentOK }, func(err error) { fatal <- err })
	if err := b.open(5, 1); err != nil {
		t.Fatal(err)
	}
	if err := b.open(5, 2); !errors.Is(err, ErrIngressOverflow) {
		t.Fatalf("overflow error = %v", err)
	}
	if got := <-fatal; !errors.Is(got, ErrIngressOverflow) {
		t.Fatalf("fatal = %v", got)
	}
	if err := b.assign(ConnectionAssignment{Generation: 5, ConnectionHandle: 1, CorrelationID: [16]byte{1}, ExpiresAt: now.Add(time.Second)}); !errors.Is(err, ErrIngressClosed) {
		t.Fatalf("assignment after overflow = %v", err)
	}
}

func TestServerNeverAcceptsCallerConstructedVerifiedClaim(t *testing.T) {
	serverTypeHasNoVerifiedIngressSetter(t)
}

func TestVerifiedFrameCannotBeConstructedOutsidePackage(t *testing.T) {
	cmd := exec.Command("go", "test", "./testdata/compilefail")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("external forged VerifiedFrame compiled successfully:\n%s", output)
	}
	if text := string(output); !strings.Contains(text, "unexported method verifiedIngressFrame") {
		t.Fatalf("compile failed for the wrong reason: %v\n%s", err, text)
	}
}

func serverTypeHasNoVerifiedIngressSetter(t *testing.T) {
	t.Helper()
	typeOfServer := reflect.TypeFor[*Server]()
	for _, forbidden := range []string{"AcceptVerifiedIngress", "SetVerifiedIngress", "PublishVerifiedIngress"} {
		if _, ok := typeOfServer.MethodByName(forbidden); ok {
			t.Fatalf("Server exposes forbidden verified-claim construction method %s", forbidden)
		}
	}
}
