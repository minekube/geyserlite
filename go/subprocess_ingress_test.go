// SPDX-License-Identifier: MIT
//go:build unix

package geyserlite

import (
	"bytes"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"go.minekube.com/connect/geyserliteabi"
)

func TestAuthenticatedPacketFrozenLayoutsAndTamperRejection(t *testing.T) {
	key := bytes.Repeat([]byte{0x5a}, geyserliteabi.SubprocessIPCKeyBytes)
	corr := [16]byte{1, 2, 3}
	bootstrap, err := encodeSubprocessBootstrap(9, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(bootstrap) != geyserliteabi.SubprocessBootstrapPacketBytes || bootstrap[0] != geyserliteabi.SubprocessFrameVersion {
		t.Fatalf("bootstrap layout = %x", bootstrap)
	}

	assignment := encodeAssignmentPacket(key, 9, 1, 44, corr, 1234)
	if len(assignment) != geyserliteabi.SubprocessAssignmentPacketBytes {
		t.Fatalf("assignment bytes = %d", len(assignment))
	}
	decoded, err := decodeAuthenticatedPacket(assignment, key, 9, 1)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.typ != geyserliteabi.SubprocessAssignment || decoded.handle != 44 || decoded.correlation != corr || decoded.expiresUnixMS != 1234 {
		t.Fatalf("decoded assignment = %+v", decoded)
	}

	ack := encodeACKPacket(key, 9, 2, 44, corr, geyserliteabi.SubprocessACKNegative)
	if len(ack) != geyserliteabi.SubprocessAssignmentACKPacketBytes {
		t.Fatalf("ACK bytes = %d", len(ack))
	}
	decoded, err = decodeAuthenticatedPacket(ack, key, 9, 2)
	if err != nil || decoded.status != geyserliteabi.SubprocessACKNegative {
		t.Fatalf("decoded ACK = %+v, %v", decoded, err)
	}

	open := encodeConnectionOpenPacket(key, 9, 3, 45)
	if len(open) != geyserliteabi.SubprocessConnectionOpenPacketBytes {
		t.Fatalf("open bytes = %d", len(open))
	}
	if _, err := decodeAuthenticatedPacket(open, key, 9, 3); err != nil {
		t.Fatal(err)
	}

	payload := append([]byte{8, 1, 18, 16}, corr[:]...)
	verified := encodeVerifiedPacket(key, 9, 4, 44, payload)
	if len(verified) != geyserliteabi.MinSubprocessVerifiedIngressPacketBytes+len(payload)-1 {
		t.Fatalf("verified bytes = %d", len(verified))
	}
	decoded, err = decodeAuthenticatedPacket(verified, key, 9, 4)
	if err != nil || !bytes.Equal(decoded.payload, payload) {
		t.Fatalf("decoded verified = %+v, %v", decoded, err)
	}

	verified[len(verified)-1] ^= 1
	if _, err := decodeAuthenticatedPacket(verified, key, 9, 4); !errors.Is(err, ErrIngressAuthentication) {
		t.Fatalf("tampered packet error = %v", err)
	}
	if _, err := decodeAuthenticatedPacket(open, key, 9, 4); !errors.Is(err, ErrIngressSequence) {
		t.Fatalf("sequence error = %v", err)
	}
}

func TestSubprocessIngressSessionOpenAssignACKAndVerified(t *testing.T) {
	parent, child, err := newTestPacketPair()
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	now := time.Unix(1_800_000_000, 0)
	key := bytes.Repeat([]byte{7}, geyserliteabi.SubprocessIPCKeyBytes)
	b := newIngressBroker(4, func() time.Time { return now })
	s := newSubprocessIngressSession(12, key, parent, b)
	b.activate(12, s.assign, s.fail)
	s.start()
	defer s.close(nil)

	if _, err := child.Write(encodeConnectionOpenPacket(key, 12, 1, 99)); err != nil {
		t.Fatal(err)
	}
	select {
	case opened := <-b.opened:
		if opened.ConnectionHandle != 99 || opened.Generation != 12 {
			t.Fatalf("opened = %+v", opened)
		}
	case <-time.After(time.Second):
		t.Fatal("connection-open was not delivered")
	}

	corr := [16]byte{9, 8, 7}
	expires := now.Add(time.Second)
	assignDone := make(chan error, 1)
	go func() {
		assignDone <- b.assign(ConnectionAssignment{Generation: 12, ConnectionHandle: 99, CorrelationID: corr, ExpiresAt: expires})
	}()
	packet := make([]byte, geyserliteabi.MaxAuthenticatedPacketBytes)
	n, err := child.Read(packet)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeAuthenticatedPacket(packet[:n], key, 12, 1)
	if err != nil || decoded.typ != geyserliteabi.SubprocessAssignment {
		t.Fatalf("assignment packet = %+v, %v", decoded, err)
	}
	if _, err := child.Write(encodeACKPacket(key, 12, 2, 99, corr, geyserliteabi.SubprocessACKPositive)); err != nil {
		t.Fatal(err)
	}
	if err := <-assignDone; err != nil {
		t.Fatal(err)
	}

	payload := append([]byte{8, 1, 18, 16}, corr[:]...)
	if _, err := child.Write(encodeVerifiedPacket(key, 12, 3, 99, payload)); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-b.verified:
		if got.ConnectionHandle() != 99 || got.CorrelationID() != corr || !bytes.Equal(got.Frame(), payload) {
			t.Fatalf("verified = handle %d corr %x frame %x", got.ConnectionHandle(), got.CorrelationID(), got.Frame())
		}
	case <-time.After(time.Second):
		t.Fatal("verified ingress was not delivered")
	}
}

func TestSubprocessIngressRejectsNegativeACKSequenceAndForgedCorrelation(t *testing.T) {
	key := bytes.Repeat([]byte{3}, geyserliteabi.SubprocessIPCKeyBytes)
	corr := [16]byte{1}
	if _, err := extractIngressCorrelation([]byte{8, 1, 18, 15}); !errors.Is(err, ErrIngressABI) {
		t.Fatalf("short correlation error = %v", err)
	}
	ack := encodeACKPacket(key, 1, 1, 1, corr, 7)
	if _, err := decodeAuthenticatedPacket(ack, key, 1, 1); !errors.Is(err, ErrIngressRejected) {
		t.Fatalf("unknown ACK status error = %v", err)
	}
	open := encodeConnectionOpenPacket(key, 1, 1, 1)
	if _, err := decodeAuthenticatedPacket(open, key, 2, 1); !errors.Is(err, ErrIngressGeneration) {
		t.Fatalf("old generation error = %v", err)
	}
}

func newTestPacketPair() (*os.File, *os.File, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(fds[0]), "test-ingress-parent"), os.NewFile(uintptr(fds[1]), "test-ingress-child"), nil
}
