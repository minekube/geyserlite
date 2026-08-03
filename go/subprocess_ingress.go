// SPDX-License-Identifier: MIT
package geyserlite

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"go.minekube.com/connect/geyserliteabi"
)

type authenticatedPacket struct {
	typ           uint8
	generation    uint64
	sequence      uint64
	handle        uint64
	correlation   [geyserliteabi.CorrelationBytes]byte
	expiresUnixMS uint64
	status        uint8
	payload       []byte
}

func encodeSubprocessBootstrap(generation uint64, key []byte) ([]byte, error) {
	if generation == 0 || len(key) != geyserliteabi.SubprocessIPCKeyBytes {
		return nil, ErrIngressABI
	}
	packet := make([]byte, geyserliteabi.SubprocessBootstrapPacketBytes)
	packet[0] = geyserliteabi.SubprocessFrameVersion
	binary.BigEndian.PutUint64(packet[1:9], generation)
	copy(packet[9:], key)
	return packet, nil
}

func authenticatedPrefix(typ uint8, generation, sequence, handle uint64, size int) []byte {
	packet := make([]byte, size)
	packet[geyserliteabi.SubprocessVersionOffset] = geyserliteabi.SubprocessFrameVersion
	packet[geyserliteabi.SubprocessTypeOffset] = typ
	binary.BigEndian.PutUint64(packet[geyserliteabi.SubprocessGenerationOffset:], generation)
	binary.BigEndian.PutUint64(packet[geyserliteabi.SubprocessSequenceOffset:], sequence)
	binary.BigEndian.PutUint64(packet[geyserliteabi.SubprocessConnectionHandleOffset:], handle)
	return packet
}

func signAuthenticatedPacket(packet, key []byte, macOffset int) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(packet[:macOffset])
	copy(packet[macOffset:], mac.Sum(nil))
	return packet
}

func encodeAssignmentPacket(key []byte, generation, sequence, handle uint64, correlation [geyserliteabi.CorrelationBytes]byte, expiresUnixMS uint64) []byte {
	packet := authenticatedPrefix(geyserliteabi.SubprocessAssignment, generation, sequence, handle, geyserliteabi.SubprocessAssignmentPacketBytes)
	copy(packet[geyserliteabi.SubprocessCorrelationOffset:], correlation[:])
	binary.BigEndian.PutUint64(packet[geyserliteabi.SubprocessAssignmentExpiresOffset:], expiresUnixMS)
	return signAuthenticatedPacket(packet, key, geyserliteabi.SubprocessAssignmentMACOffset)
}

func encodeACKPacket(key []byte, generation, sequence, handle uint64, correlation [geyserliteabi.CorrelationBytes]byte, status uint8) []byte {
	packet := authenticatedPrefix(geyserliteabi.SubprocessAssignmentACK, generation, sequence, handle, geyserliteabi.SubprocessAssignmentACKPacketBytes)
	copy(packet[geyserliteabi.SubprocessCorrelationOffset:], correlation[:])
	packet[geyserliteabi.SubprocessAssignmentACKStatusOffset] = status
	return signAuthenticatedPacket(packet, key, geyserliteabi.SubprocessAssignmentACKMACOffset)
}

func encodeConnectionOpenPacket(key []byte, generation, sequence, handle uint64) []byte {
	packet := authenticatedPrefix(geyserliteabi.SubprocessConnectionOpen, generation, sequence, handle, geyserliteabi.SubprocessConnectionOpenPacketBytes)
	return signAuthenticatedPacket(packet, key, geyserliteabi.SubprocessConnectionOpenMACOffset)
}

func encodeVerifiedPacket(key []byte, generation, sequence, handle uint64, payload []byte) []byte {
	macOffset := geyserliteabi.SubprocessVerifiedIngressPayloadOffset + len(payload)
	packet := authenticatedPrefix(geyserliteabi.SubprocessVerifiedIngress, generation, sequence, handle, macOffset+geyserliteabi.SubprocessMACBytes)
	copy(packet[geyserliteabi.SubprocessVerifiedIngressPayloadOffset:], payload)
	return signAuthenticatedPacket(packet, key, macOffset)
}

func decodeAuthenticatedPacket(packet, key []byte, generation, sequence uint64) (authenticatedPacket, error) {
	var decoded authenticatedPacket
	if len(key) != geyserliteabi.SubprocessIPCKeyBytes || len(packet) > geyserliteabi.MaxAuthenticatedPacketBytes || len(packet) < geyserliteabi.SubprocessConnectionOpenPacketBytes {
		return decoded, ErrIngressABI
	}
	typ := packet[geyserliteabi.SubprocessTypeOffset]
	macOffset := 0
	switch typ {
	case geyserliteabi.SubprocessAssignment:
		if len(packet) != geyserliteabi.SubprocessAssignmentPacketBytes {
			return decoded, ErrIngressABI
		}
		macOffset = geyserliteabi.SubprocessAssignmentMACOffset
	case geyserliteabi.SubprocessAssignmentACK:
		if len(packet) != geyserliteabi.SubprocessAssignmentACKPacketBytes {
			return decoded, ErrIngressABI
		}
		macOffset = geyserliteabi.SubprocessAssignmentACKMACOffset
	case geyserliteabi.SubprocessVerifiedIngress:
		if len(packet) < geyserliteabi.MinSubprocessVerifiedIngressPacketBytes || len(packet) > geyserliteabi.MaxSubprocessVerifiedIngressPacketBytes {
			return decoded, ErrIngressFrame
		}
		macOffset = len(packet) - geyserliteabi.SubprocessMACBytes
	case geyserliteabi.SubprocessConnectionOpen:
		if len(packet) != geyserliteabi.SubprocessConnectionOpenPacketBytes {
			return decoded, ErrIngressABI
		}
		macOffset = geyserliteabi.SubprocessConnectionOpenMACOffset
	default:
		return decoded, ErrIngressABI
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(packet[:macOffset])
	if !hmac.Equal(packet[macOffset:], mac.Sum(nil)) {
		return decoded, ErrIngressAuthentication
	}
	if packet[geyserliteabi.SubprocessVersionOffset] != geyserliteabi.SubprocessFrameVersion {
		return decoded, ErrIngressABI
	}
	decoded = authenticatedPacket{
		typ:        typ,
		generation: binary.BigEndian.Uint64(packet[geyserliteabi.SubprocessGenerationOffset:]),
		sequence:   binary.BigEndian.Uint64(packet[geyserliteabi.SubprocessSequenceOffset:]),
		handle:     binary.BigEndian.Uint64(packet[geyserliteabi.SubprocessConnectionHandleOffset:]),
	}
	if decoded.generation != generation || generation == 0 {
		return authenticatedPacket{}, ErrIngressGeneration
	}
	if decoded.sequence != sequence || sequence == 0 {
		return authenticatedPacket{}, ErrIngressSequence
	}
	if decoded.handle == 0 {
		return authenticatedPacket{}, ErrIngressMismatch
	}
	switch typ {
	case geyserliteabi.SubprocessAssignment:
		copy(decoded.correlation[:], packet[geyserliteabi.SubprocessCorrelationOffset:geyserliteabi.SubprocessAssignmentExpiresOffset])
		decoded.expiresUnixMS = binary.BigEndian.Uint64(packet[geyserliteabi.SubprocessAssignmentExpiresOffset:])
	case geyserliteabi.SubprocessAssignmentACK:
		copy(decoded.correlation[:], packet[geyserliteabi.SubprocessCorrelationOffset:geyserliteabi.SubprocessAssignmentACKStatusOffset])
		decoded.status = packet[geyserliteabi.SubprocessAssignmentACKStatusOffset]
		if decoded.status != geyserliteabi.SubprocessACKPositive && decoded.status != geyserliteabi.SubprocessACKNegative {
			return authenticatedPacket{}, ErrIngressRejected
		}
	case geyserliteabi.SubprocessVerifiedIngress:
		decoded.payload = append([]byte(nil), packet[geyserliteabi.SubprocessVerifiedIngressPayloadOffset:macOffset]...)
	}
	return decoded, nil
}

func extractIngressCorrelation(frame []byte) ([geyserliteabi.CorrelationBytes]byte, error) {
	var correlation [geyserliteabi.CorrelationBytes]byte
	seenVersion, seenCorrelation := false, false
	for len(frame) > 0 {
		tag, n := consumeVarint(frame)
		if n < 1 {
			return correlation, ErrIngressABI
		}
		frame = frame[n:]
		field, wire := tag>>3, tag&7
		if field == 0 || field > 8 {
			return correlation, ErrIngressABI
		}
		switch wire {
		case 0:
			value, size := consumeVarint(frame)
			if size < 1 {
				return correlation, ErrIngressABI
			}
			if field == 1 {
				if seenVersion || value != uint64(geyserliteabi.VerifiedIngressVersion) {
					return correlation, ErrIngressABI
				}
				seenVersion = true
			}
			frame = frame[size:]
		case 1:
			if len(frame) < 8 {
				return correlation, ErrIngressABI
			}
			frame = frame[8:]
		case 2:
			length, size := consumeVarint(frame)
			if size < 1 || length > uint64(len(frame)-size) {
				return correlation, ErrIngressABI
			}
			value := frame[size : size+int(length)]
			if field == 2 {
				if seenCorrelation || len(value) != geyserliteabi.CorrelationBytes {
					return correlation, ErrIngressMismatch
				}
				copy(correlation[:], value)
				seenCorrelation = true
			}
			frame = frame[size+int(length):]
		case 5:
			if len(frame) < 4 {
				return correlation, ErrIngressABI
			}
			frame = frame[4:]
		default:
			return correlation, ErrIngressABI
		}
	}
	if !seenVersion || !seenCorrelation || correlation == ([geyserliteabi.CorrelationBytes]byte{}) {
		return correlation, ErrIngressMismatch
	}
	return correlation, nil
}

func consumeVarint(value []byte) (uint64, int) {
	var result uint64
	for i, b := range value {
		if i == 10 || i == 9 && b > 1 {
			return 0, -1
		}
		result |= uint64(b&0x7f) << (7 * i)
		if b < 0x80 {
			return result, i + 1
		}
	}
	return 0, 0
}

type ingressPacketConn interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}

type subprocessPendingKey struct {
	handle      uint64
	correlation [geyserliteabi.CorrelationBytes]byte
}

type subprocessIngressSession struct {
	generation uint64
	key        []byte
	conn       ingressPacketConn
	broker     *ingressBroker

	writeMu       sync.Mutex
	writeSequence uint64
	opMu          sync.Mutex
	accepting     bool
	opWG          sync.WaitGroup
	mu            sync.Mutex
	waiters       map[subprocessPendingKey]chan uint8
	closeOnce     sync.Once
	closeErr      error
	done          chan struct{}
	wg            sync.WaitGroup
}

func newSubprocessIngressSession(generation uint64, key []byte, conn ingressPacketConn, broker *ingressBroker) *subprocessIngressSession {
	return &subprocessIngressSession{generation: generation, key: append([]byte(nil), key...), conn: conn, broker: broker, accepting: true, waiters: make(map[subprocessPendingKey]chan uint8), done: make(chan struct{})}
}

func (s *subprocessIngressSession) start() {
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.readLoop() }()
}

func (s *subprocessIngressSession) assign(assignment ConnectionAssignment) int32 {
	s.opMu.Lock()
	if !s.accepting {
		s.opMu.Unlock()
		return embeddedAssignmentTransportFailure
	}
	s.opWG.Add(1)
	s.opMu.Unlock()
	defer s.opWG.Done()
	if assignment.Generation != s.generation {
		return embeddedAssignmentTransportFailure
	}
	key := subprocessPendingKey{handle: assignment.ConnectionHandle, correlation: assignment.CorrelationID}
	waiter := make(chan uint8, 1)
	s.mu.Lock()
	if _, exists := s.waiters[key]; exists {
		s.mu.Unlock()
		return geyserliteabi.AssignmentDuplicateHandleOrCorrelation
	}
	s.waiters[key] = waiter
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.waiters, key); s.mu.Unlock() }()

	s.writeMu.Lock()
	s.writeSequence++
	packet := encodeAssignmentPacket(s.key, s.generation, s.writeSequence, assignment.ConnectionHandle, assignment.CorrelationID, uint64(assignment.ExpiresAt.UnixMilli()))
	err := writeFullPacket(s.conn, packet)
	s.writeMu.Unlock()
	if err != nil {
		s.fail(err)
		return embeddedAssignmentTransportFailure
	}
	timeout := time.Until(assignment.ExpiresAt)
	if timeout <= 0 || timeout > geyserliteabi.MaxIngressLifetime {
		timeout = geyserliteabi.MaxIngressLifetime
	}
	select {
	case status := <-waiter:
		if status == geyserliteabi.SubprocessACKPositive {
			return geyserliteabi.AssignmentOK
		}
		return geyserliteabi.AssignmentWrongConnectionState
	case <-time.After(timeout):
		s.fail(ErrIngressLifetime)
		return geyserliteabi.AssignmentInvalidOrExpiredTime
	case <-s.done:
		return embeddedAssignmentTransportFailure
	}
}

func (s *subprocessIngressSession) readLoop() {
	sequence := uint64(1)
	buffer := make([]byte, geyserliteabi.MaxAuthenticatedPacketBytes)
	for {
		n, err := s.conn.Read(buffer)
		if err != nil {
			if !errors.Is(err, os.ErrClosed) && !errors.Is(err, io.EOF) {
				s.fail(err)
			} else {
				s.fail(io.EOF)
			}
			return
		}
		packet, err := decodeAuthenticatedPacket(buffer[:n], s.key, s.generation, sequence)
		if err != nil {
			s.fail(err)
			return
		}
		sequence++
		switch packet.typ {
		case geyserliteabi.SubprocessConnectionOpen:
			if err := s.broker.open(s.generation, packet.handle); err != nil {
				s.fail(err)
				return
			}
		case geyserliteabi.SubprocessAssignmentACK:
			key := subprocessPendingKey{handle: packet.handle, correlation: packet.correlation}
			s.mu.Lock()
			waiter, ok := s.waiters[key]
			if ok {
				delete(s.waiters, key)
			}
			s.mu.Unlock()
			if !ok {
				s.fail(ErrIngressMismatch)
				return
			}
			waiter <- packet.status
			if packet.status == geyserliteabi.SubprocessACKNegative {
				s.fail(ErrIngressRejected)
				return
			}
		case geyserliteabi.SubprocessVerifiedIngress:
			correlation, err := extractIngressCorrelation(packet.payload)
			if err != nil {
				s.fail(err)
				return
			}
			if err := s.broker.deliverIPCFrame(s.generation, packet.handle, correlation, packet.payload); err != nil {
				s.fail(err)
				return
			}
		default:
			s.fail(ErrIngressABI)
			return
		}
	}
}

func (s *subprocessIngressSession) fail(reason error) { s.close(reason) }

func (s *subprocessIngressSession) close(reason error) {
	s.closeOnce.Do(func() {
		s.opMu.Lock()
		s.accepting = false
		s.opMu.Unlock()
		s.mu.Lock()
		s.closeErr = reason
		s.mu.Unlock()
		s.broker.deactivate(s.generation)
		close(s.done)
		_ = s.conn.Close()
		s.mu.Lock()
		for _, waiter := range s.waiters {
			select {
			case waiter <- geyserliteabi.SubprocessACKNegative:
			default:
			}
		}
		clear(s.waiters)
		s.mu.Unlock()
	})
}

func (s *subprocessIngressSession) wait() {
	s.wg.Wait()
	s.opWG.Wait()
	for i := range s.key {
		s.key[i] = 0
	}
}

func (s *subprocessIngressSession) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeErr
}

func writeFullPacket(conn io.Writer, packet []byte) error {
	n, err := conn.Write(packet)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("geyserlite: short ingress packet write: %d/%d", n, len(packet))
	}
	return nil
}
