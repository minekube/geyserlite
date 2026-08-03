package compilefail

import (
	"time"

	geyserlite "go.minekube.com/geyserlite"
)

type forgedVerifiedFrame struct{}

func (forgedVerifiedFrame) Generation() uint64       { return 1 }
func (forgedVerifiedFrame) ConnectionHandle() uint64 { return 1 }
func (forgedVerifiedFrame) CorrelationID() [16]byte  { return [16]byte{1} }
func (forgedVerifiedFrame) Frame() []byte            { return []byte{8, 1} }
func (forgedVerifiedFrame) ExpiresAt() time.Time     { return time.Now().Add(time.Second) }
func (forgedVerifiedFrame) verifiedIngressFrame()    {}

var _ geyserlite.VerifiedFrame = forgedVerifiedFrame{}
