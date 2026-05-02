// SPDX-License-Identifier: MIT
package synthetic

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBuildPing(t *testing.T) {
	t.Parallel()
	pkt := buildPing(time.UnixMilli(1234567890))
	if len(pkt) != 33 {
		t.Fatalf("len=%d, want 33", len(pkt))
	}
	if pkt[0] != idUnconnectedPing {
		t.Errorf("id=%#x", pkt[0])
	}
	if !equalBytes(pkt[9:25], rakNetMagic) {
		t.Error("magic mismatch")
	}
	ts := binary.BigEndian.Uint64(pkt[1:9])
	if ts != 1234567890 {
		t.Errorf("ts=%d", ts)
	}
}

func TestParsePongValid(t *testing.T) {
	t.Parallel()
	desc := "MCPE;Minekube;800;1.21.50;0;100;1234567890123456;Sub MOTD;Survival;1;19132;19133"
	pong := buildSyntheticPong(0xdeadbeefcafef00d, desc)

	m, err := parsePong(pong)
	if err != nil {
		t.Fatal(err)
	}
	if m.Edition != "MCPE" {
		t.Errorf("Edition=%q", m.Edition)
	}
	if m.Line1 != "Minekube" {
		t.Errorf("Line1=%q", m.Line1)
	}
	if m.ProtocolVer != 800 {
		t.Errorf("Protocol=%d", m.ProtocolVer)
	}
	if m.GameVersion != "1.21.50" {
		t.Errorf("GameVersion=%q", m.GameVersion)
	}
	if m.Players != 0 || m.MaxPlayers != 100 {
		t.Errorf("Players=%d/%d", m.Players, m.MaxPlayers)
	}
	if m.Line2 != "Sub MOTD" {
		t.Errorf("Line2=%q", m.Line2)
	}
	if m.GameMode != "Survival" || m.GameModeNum != 1 {
		t.Errorf("GameMode=%q/%d", m.GameMode, m.GameModeNum)
	}
	if m.PortV4 != 19132 || m.PortV6 != 19133 {
		t.Errorf("ports=%d/%d", m.PortV4, m.PortV6)
	}
	if !strings.HasSuffix(m.ServerGUID, "cafef00d") {
		t.Errorf("ServerGUID=%q", m.ServerGUID)
	}
	if m.RawDescriptor != desc {
		t.Errorf("RawDescriptor differs")
	}
}

func TestParsePongShort(t *testing.T) {
	t.Parallel()
	if _, err := parsePong([]byte{0x1c, 0, 0}); err == nil {
		t.Error("expected error for short pong")
	}
}

func TestParsePongBadMagic(t *testing.T) {
	t.Parallel()
	pong := buildSyntheticPong(1, "MCPE;Test;800;1.21.50;0;1;1;sub;Survival;1;19132;19133")
	// corrupt magic
	pong[17] = 0xff
	if _, err := parsePong(pong); err == nil {
		t.Error("expected error for bad magic")
	}
}

// TestProbeAgainstFakeServer spins up a tiny UDP listener that mimics a
// Bedrock unconnected-pong, then verifies Probe parses it correctly.
// No real Geyser involved — pure RakNet wire protocol test.
func TestProbeAgainstFakeServer(t *testing.T) {
	t.Parallel()
	srv, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	go func() {
		buf := make([]byte, 1024)
		_, addr, err := srv.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pong := buildSyntheticPong(42, "MCPE;Synthetic;800;1.21.50;0;100;42;sub;Survival;1;19132;19133")
		_, _ = srv.WriteToUDP(pong, addr)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	motd, err := Probe(ctx, srv.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	if motd.Line1 != "Synthetic" {
		t.Errorf("Line1=%q", motd.Line1)
	}
	if motd.RTTMillis <= 0 {
		t.Errorf("RTT=%v should be > 0", motd.RTTMillis)
	}
}

// buildSyntheticPong constructs an Unconnected Pong byte payload for tests.
func buildSyntheticPong(serverGUID uint64, descriptor string) []byte {
	out := make([]byte, 0, 35+len(descriptor))
	out = append(out, idUnconnectedPong)
	out = append(out, make([]byte, 8)...) // timestamp echo (zero is fine)
	guid := make([]byte, 8)
	binary.BigEndian.PutUint64(guid, serverGUID)
	out = append(out, guid...)
	out = append(out, rakNetMagic...)
	strLen := make([]byte, 2)
	binary.BigEndian.PutUint16(strLen, uint16(len(descriptor)))
	out = append(out, strLen...)
	out = append(out, []byte(descriptor)...)
	return out
}
