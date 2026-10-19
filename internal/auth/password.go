package auth

import (
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"server/internal/db"

	protocol "github.com/Ryo-del/Syne-protocol"
	"github.com/libp2p/go-libp2p/core/network"
)

func (a *AuthHandler) handlePasswordChange(stream network.Stream, data []byte) {
	request, err := protocol.UnmarshalJSON[protocol.PasswordChangeRequest](data)
	if err != nil || request.SessionID == "" {
		a.writePasswordChangeResult(stream, false, "invalid request")
		return
	}
	peerID := stream.Conn().RemotePeer().String()
	info, err := db.GetSessionInfo(a.DB, request.SessionID)
	if err != nil || info.PeerID != peerID || time.Now().Unix() > info.ExpiresAt {
		a.writePasswordChangeResult(stream, false, "invalid session")
		return
	}
	user, err := db.GetUserByLogin(a.DB, info.Login)
	if err != nil {
		slog.Error("password change: load user failed", "err", err)
		a.writePasswordChangeResult(stream, false, "internal error")
		return
	}
	challenge, err := protocol.MarshalJSON(protocol.PasswordChangeChallenge{
		Type: protocol.AuthTypePasswordChangeChallenge, PasswordSalt: user.PasswordSalt,
	})
	if err != nil || protocol.WriteFramedMessage(stream, challenge) != nil {
		return
	}
	verifyData, err := protocol.ReadFramedMessage(stream)
	if err != nil {
		return
	}
	verify, err := protocol.UnmarshalJSON[protocol.PasswordChangeVerify](verifyData)
	if err != nil || verify.Type != protocol.AuthTypePasswordChangeVerify || verify.SessionID != request.SessionID {
		a.writePasswordChangeResult(stream, false, "invalid request")
		return
	}
	err = db.ChangePasswordForSession(a.DB, request.SessionID, peerID,
		verify.CurrentPasswordHash, verify.PasswordHash, verify.PasswordSalt, verify.LoginKeySalt,
		verify.EncryptedMasterKey, verify.IdentityPublicKey, verify.EncryptedIdentityKey)
	switch {
	case errors.Is(err, db.ErrInvalidCurrentPassword):
		a.writePasswordChangeResult(stream, false, "invalid current password")
	case errors.Is(err, db.ErrInvalidSession), errors.Is(err, sql.ErrNoRows):
		a.writePasswordChangeResult(stream, false, "invalid session")
	case err != nil:
		slog.Error("password change: update failed", "err", err)
		a.writePasswordChangeResult(stream, false, "internal error")
	default:
		a.writePasswordChangeResult(stream, true, "")
	}
}

func (a *AuthHandler) writePasswordChangeResult(stream network.Stream, ok bool, reason string) {
	data, err := protocol.MarshalJSON(protocol.PasswordChangeResult{
		Type: protocol.AuthTypePasswordChangeResult, OK: ok, Reason: reason,
	})
	if err != nil {
		return
	}
	if err := protocol.WriteFramedMessage(stream, data); err != nil {
		slog.Error("password change: response write failed", "err", err)
	}
}
