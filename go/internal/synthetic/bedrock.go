// SPDX-License-Identifier: MIT

// Package synthetic implements a tiny RakNet client that probes a Bedrock
// server for its MOTD using only an Unconnected Ping. Used by CI smoke
// tests so we don't need a real Bedrock client (or Mojang account) to
// validate that geyserlite is alive after a build.
package synthetic

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// RakNet packet IDs we care about.
const (
	idUnconnectedPing = 0x01
	idUnconnectedPong = 0x1c
)

// rakNetMagic is the fixed magic blob every RakNet offline packet carries.
var rakNetMagic = []byte{
	0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe,
	0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78,
}

// MOTD is the parsed Unconnected Pong response from a Bedrock server.
//
// Format reference: the semicolon-separated string Bedrock servers emit;
// see also github.com/GeyserMC/Geyser docs. Not every field is always present;
// fields beyond the first ~6 may be empty for older or stripped-down servers.
type MOTD struct {
	Edition       string // typically "MCPE"
	Line1         string // primary MOTD
	ProtocolVer   int    // Bedrock protocol version
	GameVersion   string // e.g. "1.21.50"
	Players       int
	MaxPlayers    int
	ServerGUID    string
	Line2         string // secondary MOTD / sub-line
	GameMode      string // "Survival", "Creative", etc.
	GameModeNum   int
	PortV4        int
	PortV6        int
	RTTMillis     float64 // round-trip time of the probe
	RawDescriptor string  // the raw semicolon-separated string (debug aid)
}

// Probe sends an Unconnected Ping to addr ("host:port") and returns the
// decoded MOTD. The deadline is taken from ctx (else 3s).
func Probe(ctx context.Context, addr string) (*MOTD, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(3 * time.Second)
	}

	d := net.Dialer{Timeout: time.Until(deadline)}
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return nil, fmt.Errorf("synthetic: dial %s: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	t0 := time.Now()
	if _, err := conn.Write(buildPing(t0)); err != nil {
		return nil, fmt.Errorf("synthetic: write ping: %w", err)
	}

	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("synthetic: read pong: %w", err)
	}
	rtt := time.Since(t0)

	motd, err := parsePong(buf[:n])
	if err != nil {
		return nil, err
	}
	motd.RTTMillis = float64(rtt.Microseconds()) / 1000.0
	return motd, nil
}

func buildPing(t time.Time) []byte {
	pkt := make([]byte, 33)
	pkt[0] = idUnconnectedPing
	binary.BigEndian.PutUint64(pkt[1:9], uint64(t.UnixMilli()))
	copy(pkt[9:25], rakNetMagic)
	binary.BigEndian.PutUint64(pkt[25:33], 1) // arbitrary client GUID
	return pkt
}

func parsePong(data []byte) (*MOTD, error) {
	// Layout: 1B id | 8B timestamp | 8B serverGUID | 16B magic | 2B strlen | string
	if len(data) < 35 {
		return nil, fmt.Errorf("synthetic: pong too short (%d bytes)", len(data))
	}
	if data[0] != idUnconnectedPong {
		return nil, fmt.Errorf("synthetic: expected pong id 0x1c, got %#x", data[0])
	}
	serverGUID := binary.BigEndian.Uint64(data[9:17])
	gotMagic := data[17:33]
	if !equalBytes(gotMagic, rakNetMagic) {
		return nil, errors.New("synthetic: bad RakNet magic in pong")
	}
	strLen := binary.BigEndian.Uint16(data[33:35])
	if int(35+strLen) > len(data) {
		return nil, fmt.Errorf("synthetic: pong descriptor truncated")
	}
	desc := string(data[35 : 35+strLen])

	parts := strings.Split(desc, ";")
	m := &MOTD{ServerGUID: fmt.Sprintf("%016x", serverGUID), RawDescriptor: desc}
	if len(parts) > 0 {
		m.Edition = parts[0]
	}
	if len(parts) > 1 {
		m.Line1 = parts[1]
	}
	if len(parts) > 2 {
		m.ProtocolVer, _ = strconv.Atoi(parts[2])
	}
	if len(parts) > 3 {
		m.GameVersion = parts[3]
	}
	if len(parts) > 4 {
		m.Players, _ = strconv.Atoi(parts[4])
	}
	if len(parts) > 5 {
		m.MaxPlayers, _ = strconv.Atoi(parts[5])
	}
	// parts[6] is the server GUID per the protocol; the binary header
	// already gave us a more reliable copy, so we skip parts[6] entirely.
	if len(parts) > 7 {
		m.Line2 = parts[7]
	}
	if len(parts) > 8 {
		m.GameMode = parts[8]
	}
	if len(parts) > 9 {
		m.GameModeNum, _ = strconv.Atoi(parts[9])
	}
	if len(parts) > 10 {
		m.PortV4, _ = strconv.Atoi(parts[10])
	}
	if len(parts) > 11 {
		m.PortV6, _ = strconv.Atoi(parts[11])
	}
	return m, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Wait probes addr repeatedly until either a pong is received or the
// context expires. Useful as a CI smoke step right after starting a
// container — the bedrock listener may take a moment to bind.
func Wait(ctx context.Context, addr string, pollEvery time.Duration) (*MOTD, error) {
	if pollEvery <= 0 {
		pollEvery = 250 * time.Millisecond
	}
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		motd, err := Probe(probeCtx, addr)
		cancel()
		if err == nil {
			return motd, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("synthetic: wait timed out: %w", ctx.Err())
		case <-time.After(pollEvery):
		}
	}
}
