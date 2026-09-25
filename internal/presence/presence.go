package presence

import (
	"io"
	"sync"

	protocol "github.com/Ryo-del/Syne-protocol"
	"github.com/libp2p/go-libp2p/core/network"
)

type presenceClient struct {
	stream  network.Stream
	writeMu sync.Mutex
	user    protocol.PresenceUser
}

type PresenceHandler struct {
	mu      sync.RWMutex
	clients map[string]*presenceClient // key = UserID
}

func NewPresenceHandler() *PresenceHandler {
	return &PresenceHandler{
		clients: make(map[string]*presenceClient),
	}
}

func (h *PresenceHandler) HandleStream(stream network.Stream) {
	// 1. Читаем PresenceOnline.
	data, err := protocol.ReadFramedMessage(stream)
	if err != nil {
		stream.Close()
		return
	}

	online, err := protocol.UnmarshalJSON[protocol.PresenceOnline](data)
	if err != nil {
		stream.Close()
		return
	}

	userID := online.UserID
	if userID == "" {
		stream.Close()
		return
	}

	client := &presenceClient{
		stream: stream,
		user: protocol.PresenceUser{
			UserID: userID,
			PeerID: online.PeerID,
			FName:  online.FName,
			SName:  online.SName,
		},
	}

	// 2. Регистрируем клиента.
	h.mu.Lock()

	// Если UserID уже зарегистрирован (например, переподключение после
	// разрыва сети), закрываем старое соединение — новое главнее.
	if oldClient, exists := h.clients[userID]; exists {
		oldClient.stream.Close()
	}

	h.clients[userID] = client

	// 3. Собираем snapshot — уже с полными данными, а не только с ID.
	snapshot := make([]protocol.PresenceUser, 0, len(h.clients)-1)
	for id, c := range h.clients {
		if id == userID {
			continue
		}
		snapshot = append(snapshot, c.user)
	}

	h.mu.Unlock()

	// 4. Отправляем snapshot новому клиенту.
	snapshotMessage := protocol.PresenceSnapshot{
		Type:  protocol.PresenceTypeSnapshot,
		Users: snapshot,
	}

	if err := writeMessage(client, snapshotMessage); err != nil {
		h.removeClient(userID, client)
		stream.Close()
		return
	}

	// 5. Сообщаем остальным, что пользователь online.
	h.broadcastUpdate(
		userID,
		protocol.PresenceUpdate{
			Type:   protocol.PresenceTypeUpdate,
			User:   client.user,
			Status: protocol.PresenceStatusOnline,
		},
	)

	_, _ = io.Copy(io.Discard, stream)

	// 6. Удаляем клиента и сообщаем остальным об offline.
	if h.removeClient(userID, client) {
		h.broadcastUpdate(
			userID,
			protocol.PresenceUpdate{
				Type:   protocol.PresenceTypeUpdate,
				User:   client.user,
				Status: protocol.PresenceStatusOffline,
			},
		)
	}

	stream.Close()
}

func (h *PresenceHandler) broadcastUpdate(
	excludeUserID string,
	update protocol.PresenceUpdate,
) {
	h.mu.Lock()

	clients := make([]*presenceClient, 0, len(h.clients))

	for userID, client := range h.clients {
		if userID == excludeUserID {
			continue
		}

		clients = append(clients, client)
	}

	h.mu.Unlock()

	for _, client := range clients {
		_ = writeMessage(client, update)
	}
}

func (h *PresenceHandler) removeClient(
	userID string,
	client *presenceClient,
) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	current, exists := h.clients[userID]
	if !exists {
		return false
	}

	if current != client {
		return false
	}

	delete(h.clients, userID)

	return true
}

func writeMessage(
	client *presenceClient,
	message any,
) error {
	data, err := protocol.MarshalJSON(message)
	if err != nil {
		return err
	}

	client.writeMu.Lock()
	defer client.writeMu.Unlock()

	return protocol.WriteFramedMessage(client.stream, data)
}
