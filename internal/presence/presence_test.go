package presence

import (
	"testing"
	"time"

	protocol "github.com/Ryo-del/Syne-protocol"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

func newPresenceTestHost(t *testing.T) network.Stream {
	t.Helper()
	return nil // placeholder, заменяется ниже
}

func connectAndGoOnline(t *testing.T, client host.Host, serverID peer.ID, userID string) network.Stream {
	t.Helper()

	stream, err := client.NewStream(t.Context(), serverID, protocol.PresenceStreamProtocol)
	if err != nil {
		t.Fatalf("open stream for %s: %v", userID, err)
	}

	msg := protocol.PresenceOnline{Type: protocol.PresenceTypeOnline, UserID: userID}
	data, err := protocol.MarshalJSON(msg)
	if err != nil {
		t.Fatalf("marshal online for %s: %v", userID, err)
	}
	if err := protocol.WriteFramedMessage(stream, data); err != nil {
		t.Fatalf("write online for %s: %v", userID, err)
	}

	return stream
}

func readSnapshot(t *testing.T, stream network.Stream) protocol.PresenceSnapshot {
	t.Helper()

	data, err := protocol.ReadFramedMessage(stream)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	snap, err := protocol.UnmarshalJSON[protocol.PresenceSnapshot](data)
	if err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	return snap
}

func readUpdate(t *testing.T, stream network.Stream) protocol.PresenceUpdate {
	t.Helper()

	_ = stream.SetReadDeadline(time.Now().Add(3 * time.Second))
	data, err := protocol.ReadFramedMessage(stream)
	if err != nil {
		t.Fatalf("read update: %v", err)
	}
	upd, err := protocol.UnmarshalJSON[protocol.PresenceUpdate](data)
	if err != nil {
		t.Fatalf("unmarshal update: %v", err)
	}
	return upd
}

func TestPresenceHandler_HandleStream(t *testing.T) {
	server, err := libp2p.New()
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	handler := NewPresenceHandler()
	server.SetStreamHandler(protocol.PresenceStreamProtocol, handler.HandleStream)

	client1, err := libp2p.New()
	if err != nil {
		t.Fatalf("create client1: %v", err)
	}
	t.Cleanup(func() { _ = client1.Close() })

	client2, err := libp2p.New()
	if err != nil {
		t.Fatalf("create client2: %v", err)
	}
	t.Cleanup(func() { _ = client2.Close() })

	serverInfo := peer.AddrInfo{ID: server.ID(), Addrs: server.Addrs()}
	if err := client1.Connect(t.Context(), serverInfo); err != nil {
		t.Fatalf("connect client1: %v", err)
	}
	if err := client2.Connect(t.Context(), serverInfo); err != nil {
		t.Fatalf("connect client2: %v", err)
	}

	// user-1 подключается первым — снапшот должен быть пустым.
	stream1 := connectAndGoOnline(t, client1, server.ID(), "user-1")
	t.Cleanup(func() { _ = stream1.Close() })

	snap1 := readSnapshot(t, stream1)
	if len(snap1.Users) != 0 {
		t.Fatalf("user-1 snapshot: got %d users, want 0", len(snap1.Users))
	}

	// user-2 подключается вторым — должен увидеть user-1 в снапшоте,
	// а user-1 должен получить update "user-2 online".
	stream2 := connectAndGoOnline(t, client2, server.ID(), "user-2")

	snap2 := readSnapshot(t, stream2)
	if len(snap2.Users) != 1 || snap2.Users[0] != "user-1" {
		t.Fatalf("user-2 snapshot: got %v, want [user-1]", snap2.Users)
	}

	upd := readUpdate(t, stream1)
	if upd.UserID != "user-2" || upd.Status != protocol.PresenceStatusOnline {
		t.Fatalf("got update %+v, want user-2 online", upd)
	}

	// user-2 отключается — user-1 должен получить update "user-2 offline".
	if err := stream2.Close(); err != nil {
		t.Fatalf("close stream2: %v", err)
	}

	upd = readUpdate(t, stream1)
	if upd.UserID != "user-2" || upd.Status != protocol.PresenceStatusOffline {
		t.Fatalf("got update %+v, want user-2 offline", upd)
	}
}
