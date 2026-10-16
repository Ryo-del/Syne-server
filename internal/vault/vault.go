package vault

import (
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"server/internal/db"

	protocol "github.com/Ryo-del/Syne-protocol"
	"github.com/libp2p/go-libp2p/core/network"
	manet "github.com/multiformats/go-multiaddr/net"
)

const (
	maxMailboxMessage = 512 * 1024
	mailboxBatch      = 100
)
const groupBatch = 200

type Handler struct {
	DB *sql.DB
}

func NewHandler(database *sql.DB) *Handler {
	return &Handler{DB: database}
}

func (h *Handler) HandleStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(60 * time.Second))

	data, err := protocol.ReadFramedMessageMax(stream, protocol.SyncMaxFrame)
	if err != nil {
		slog.Error("sync: failed to read request", "err", err)
		return
	}
	req, err := protocol.UnmarshalJSON[protocol.SyncRequest](data)
	if err != nil {
		slog.Error("sync: failed to unmarshal request", "err", err)
		return
	}

	remoteIP := "unknown"
	if ip, err := manet.ToIP(stream.Conn().RemoteMultiaddr()); err == nil {
		remoteIP = ip.String()
	}
	resp := h.handle(stream.Conn().RemotePeer().String(), req, remoteIP)
	resp.Type = protocol.SyncTypeResponse

	out, err := protocol.MarshalJSON(resp)
	if err != nil {
		slog.Error("sync: failed to marshal response", "err", err)
		return
	}
	if err := protocol.WriteFramedMessage(stream, out); err != nil {
		slog.Error("sync: failed to write response", "err", err)
	}
}

func (h *Handler) handle(remotePeer string, req protocol.SyncRequest, remoteIP string) protocol.SyncResponse {
	fail := func(msg string) protocol.SyncResponse {
		return protocol.SyncResponse{Error: msg}
	}

	info, err := db.GetSessionInfo(h.DB, req.SessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return fail("invalid session")
	}
	if err != nil {
		slog.Error("sync: session lookup failed", "err", err)
		return fail("internal error")
	}
	if time.Now().Unix() > info.ExpiresAt {
		return fail("session expired")
	}
	// Сессия принадлежит конкретному устройству (peer), выдавшему её при входе.
	if info.PeerID != remotePeer {
		return fail("session belongs to another peer")
	}

	ok := protocol.SyncResponse{OK: true}

	switch req.Type {
	case protocol.SyncOpGroupList:
		groups, err := db.GroupsForUser(h.DB, info.Login)
		if err != nil {
			slog.Error("sync: group list failed", "err", err)
			return fail("internal error")
		}
		for _, g := range groups {
			ok.Groups = append(ok.Groups, protocol.GroupInfo{ID: g.ID, Name: g.Name, Emoji: g.Emoji, Key: g.Key})
		}

	case protocol.SyncOpGroupSend:
		if len(req.Data) == 0 || len(req.Data) > maxMailboxMessage {
			return fail("bad message size")
		}
		allowed, err := db.CanAccessGroup(h.DB, info.Login, req.GroupID)
		if err != nil {
			slog.Error("sync: group access check failed", "err", err)
			return fail("internal error")
		}
		if !allowed {
			return fail("not a member of this group")
		}
		msg, err := protocol.UnmarshalMessage(req.Data)
		if err != nil {
			return fail("invalid message")
		}
		if err := protocol.ValidateMessage(msg); err != nil {
			return fail("invalid message: " + err.Error())
		}
		gid := strconv.FormatInt(req.GroupID, 10)
		if msg.Type != protocol.MsgChat || msg.Target != protocol.TargetGroup ||
			msg.TargetID != gid || msg.ChatID != "grp:"+gid ||
			msg.FromUser != info.Login || msg.From != info.PeerID {
			return fail("message does not match the group or sender")
		}
		if err := db.PushGroupMessage(h.DB, req.GroupID, info.Login, msg.ID, req.Data); err != nil {
			slog.Error("sync: group push failed", "err", err)
			return fail("internal error")
		}

	case protocol.SyncOpGroupFetch:
		allowed, err := db.CanAccessGroup(h.DB, info.Login, req.GroupID)
		if err != nil {
			slog.Error("sync: group access check failed", "err", err)
			return fail("internal error")
		}
		if !allowed {
			return fail("not a member of this group")
		}
		rows, err := db.FetchGroupMessages(h.DB, req.GroupID, req.AfterID, groupBatch)
		if err != nil {
			slog.Error("sync: group fetch failed", "err", err)
			return fail("internal error")
		}
		for _, r := range rows {
			ok.Items = append(ok.Items, protocol.MailboxItem{ID: r.ID, Data: r.Data})
		}
	case protocol.SyncOpVaultGet:
		data, exists, err := db.GetVault(h.DB, info.Login)
		if err != nil {
			slog.Error("sync: vault get failed", "err", err)
			return fail("internal error")
		}
		ok.Exists = exists
		ok.Data = data

	case protocol.SyncOpVaultPut:
		if len(req.Data) == 0 {
			return fail("empty vault")
		}
		if err := db.PutVault(h.DB, info.Login, req.Data); err != nil {
			slog.Error("sync: vault put failed", "err", err)
			return fail("internal error")
		}

	case protocol.SyncOpKeyLookup:
		fname, sname, pub, err := db.GetUserKeys(h.DB, req.UserID)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("user not found")
		}
		if err != nil {
			slog.Error("sync: key lookup failed", "err", err)
			return fail("internal error")
		}
		if len(pub) == 0 {
			return fail("user has no keys yet")
		}
		ok.FName, ok.SName, ok.IdentityPublicKey = fname, sname, pub

	case protocol.SyncOpMailboxPush:
		if req.UserID == "" || len(req.Data) == 0 {
			return fail("user_id and data are required")
		}
		if len(req.Data) > maxMailboxMessage {
			return fail("message too large")
		}
		if _, _, _, err := db.GetUserKeys(h.DB, req.UserID); errors.Is(err, sql.ErrNoRows) {
			return fail("user not found")
		} else if err != nil {
			slog.Error("sync: recipient lookup failed", "err", err)
			return fail("internal error")
		}
		if err := db.PushMailbox(h.DB, req.UserID, info.Login, req.Data); err != nil {
			slog.Error("sync: mailbox push failed", "err", err)
			return fail("internal error")
		}

	case protocol.SyncOpMailboxFetch:
		rows, err := db.FetchMailbox(h.DB, info.Login, mailboxBatch)
		if err != nil {
			slog.Error("sync: mailbox fetch failed", "err", err)
			return fail("internal error")
		}
		for _, r := range rows {
			ok.Items = append(ok.Items, protocol.MailboxItem{ID: r.ID, Data: r.Data})
		}

	case protocol.SyncOpMailboxAck:
		if err := db.AckMailbox(h.DB, info.Login, req.IDs); err != nil {
			slog.Error("sync: mailbox ack failed", "err", err)
			return fail("internal error")
		}

	case protocol.SyncOpLogout:
		if err := db.DeleteSession(h.DB, req.SessionID); err != nil {
			slog.Error("sync: logout failed", "err", err)
			return fail("internal error")
		}

	case "error_report":
		message := strings.TrimSpace(string(req.Data))
		if message == "" || len(message) > 10000 {
			return fail("error report must contain 1 to 10000 characters")
		}
		fname, sname, _, err := db.GetUserKeys(h.DB, info.Login)
		if err != nil {
			slog.Error("sync: error report user lookup failed", "err", err)
			return fail("internal error")
		}
		if err := db.AddErrorReport(h.DB, info.Login, strings.TrimSpace(fname+" "+sname), remoteIP, message); err != nil {
			slog.Error("sync: error report save failed", "err", err)
			return fail("internal error")
		}

	default:
		return fail("unknown operation")
	}

	return ok
}
