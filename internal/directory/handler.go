package directory

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"

	db "server/internal/db"
)

// ВАЖНО: должно дословно совпадать с directoryProtocol в клиенте
// (Syne/core/transport/p2p/directory.go).
const ProtocolID = protocol.ID("/syne/directory/1.0.0")

type request struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
	Limit     int    `json:"limit"`
}

type user struct {
	Login string `json:"login"`
	FName string `json:"fname"`
	SName string `json:"sname"`
	Role  string `json:"role"`
}

type response struct {
	Users []user `json:"users"`
	Error string `json:"error,omitempty"`
}

type Handler struct{ database *sql.DB }

func NewHandler(database *sql.DB) *Handler { return &Handler{database: database} }

func (h *Handler) HandleStream(s network.Stream) {
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(10 * time.Second))
	enc := json.NewEncoder(s)

	var req request
	if err := json.NewDecoder(io.LimitReader(s, 4096)).Decode(&req); err != nil {
		_ = enc.Encode(response{Error: "invalid request"})
		return
	}

	me, err := db.LoginBySession(h.database, req.SessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_ = enc.Encode(response{Error: "unauthorized"})
		} else {
			slog.Error("directory: session lookup failed", "error", err)
			_ = enc.Encode(response{Error: "internal error"})
		}
		return
	}

	found, err := db.SearchDirectory(h.database, me, req.Query, req.Limit)
	if err != nil {
		slog.Error("directory: search failed", "error", err)
		_ = enc.Encode(response{Error: "internal error"})
		return
	}

	out := make([]user, 0, len(found))
	for _, u := range found {
		out = append(out, user{Login: u.Login, FName: u.FName, SName: u.SName, Role: u.Role})
	}
	_ = enc.Encode(response{Users: out})
}
