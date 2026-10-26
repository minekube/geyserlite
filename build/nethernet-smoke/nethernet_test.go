// Package nethernet_test exercises the actual native library and its Java
// callbacks. A listening HTTP socket alone cannot detect missing JNI metadata.
package nethernet_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/df-mc/go-nethernet"
	"github.com/df-mc/go-nethernet/endpoint"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestNativeNetherNetNetworkSettings(t *testing.T) {
	address := os.Getenv("GEYSERLITE_NETHERNET_SMOKE_ADDRESS")
	if address == "" {
		t.Skip("requires a running native GeyserLite with the isolated smoke config")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/v1/join", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("signaling status: %d", response.StatusCode)
	}
	var status struct {
		Protocol uint32 `json:"protocol"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Protocol == 0 {
		t.Fatal("signaling did not advertise a Bedrock protocol")
	}
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// This ephemeral self-signed identity works only with the smoke fixture's
	// validate-bedrock-login=false. Production must retain Xbox validation.
	publicKey, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES384, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.Signed(signer).Claims(map[string]any{
		"cpk": base64.StdEncoding.EncodeToString(publicKey),
		"exp": time.Now().Add(time.Minute).Unix(),
		"xid": "0", "xname": "NativeSmoke",
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	identity := &nethernet.Identity{PrivateKey: key, Token: token, Domain: "self"}
	dialer := nethernet.Dialer{Identity: identity}
	conn, err := dialer.DialContext(ctx, address, endpoint.NewClient())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	// NetherNet carries an uncompressed Bedrock batch without RakNet's 0xfe
	// marker: packet length 6, RequestNetworkSettings (193), big-endian protocol.
	request := []byte{6, 0xc1, 1, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(request[3:], status.Protocol)
	if _, err := conn.Write(request); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	length, offset := binary.Uvarint(buf[:n])
	if offset <= 0 || length > uint64(n-offset) {
		t.Fatal("invalid Bedrock batch received over NetherNet")
	}
	header, headerBytes := binary.Uvarint(buf[offset:n])
	if headerBytes <= 0 || header&0x3ff != 143 { // NetworkSettings
		t.Fatalf("expected NetworkSettings over WebRTC, got packet %d", header&0x3ff)
	}
	t.Logf("native WebRTC round trip: protocol=%d NetworkSettings received", status.Protocol)
}
