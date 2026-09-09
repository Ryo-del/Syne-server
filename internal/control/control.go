package server

import (
	"fmt"
	"io"
	"log"
	"time"

	protocol "github.com/Ryo-del/Syne-protocol"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
)

type ControlHandler struct {
	Host host.Host
}

func (c *ControlHandler) HandleStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetReadDeadline(time.Now().Add(protocol.DefaultReadDeadline))
	data, err := io.ReadAll(io.LimitReader(stream, protocol.DefaultReadLimit))
	if err != nil {
		return
	}

	hello, err := protocol.UnmarshalHello(data)
	if err != nil {
		return
	}
	if hello.ProtocolVersion != protocol.ProtocolVersion {
		rej := protocol.Reject{
			Type:      protocol.ControlTypeReject,
			Reason:    fmt.Sprintf("unsupported protocol version: got %d, expected %d", hello.ProtocolVersion, protocol.ProtocolVersion),
			Timestamp: time.Now().UnixMilli(),
		}
		log.Printf("control: rejected peer=%s reason=%q", stream.Conn().RemotePeer(), rej.Reason)
		dataReject, err := protocol.MarshalReject(rej)
		if err != nil {
			return
		}
		_, err = stream.Write(dataReject)
		if err != nil {
			return
		}
	} else {
		welcome := protocol.Welcome{
			Type:          protocol.ControlTypeWelcome,
			ServerID:      c.Host.ID().String(),
			ServerVersion: "0.1.0-dev",
			Timestamp:     time.Now().UnixMilli(),
		}

		dataWelcome, err := protocol.MarshalWelcome(welcome)
		if err != nil {
			return
		}

		_, err = stream.Write(dataWelcome)
		if err != nil {
			return
		}
	}
}
func NewControlHandler(h host.Host) *ControlHandler {
	return &ControlHandler{Host: h}
}
