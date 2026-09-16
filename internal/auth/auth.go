package auth

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"log/slog"
	"server/internal/db"
	"time"

	protocol "github.com/Ryo-del/Syne-protocol"
	"github.com/libp2p/go-libp2p/core/network"
)

type AuthHandler struct {
	DB *sql.DB
}

func NewAuthHandler(db *sql.DB) *AuthHandler {
	return &AuthHandler{DB: db}
}

func (a *AuthHandler) HandleStream(stream network.Stream) {
	defer stream.Close()

	data, err := protocol.ReadFramedMessage(stream)
	if err != nil {
		slog.Error("failed to read framed message", "err", err)
		return
	}

	msgType, err := protocol.PeekType(data)
	if err != nil {
		slog.Error("failed to peek message type", "err", err)
		return
	}

	switch msgType {
	case protocol.AuthTypeRegisterRequest:
		a.handleRegister(stream, data)

	case protocol.AuthTypeLoginRequest:
		a.handleLogin(stream, data)

	default:
		slog.Error("unknown message type", "type", msgType)
	}
}

func (a *AuthHandler) handleRegister(stream network.Stream, data []byte) {
	req, err := protocol.UnmarshalJSON[protocol.RegisterRequest](data)
	if err != nil {
		slog.Error("failed to unmarshal RegisterRequest", "err", err)
		return
	}

	err = db.CreateUser(
		a.DB,
		req.Login,
		req.FName,
		req.SName,
		req.Role,
		req.PasswordHash,
		req.PasswordSalt,
		req.LoginKeySalt,
		req.EncryptedMasterKey,
		req.IdentityPublicKey,
		req.EncryptedIdentityKey,
	)

	if err != nil {
		reason := "internal error"

		if errors.Is(err, db.ErrLoginAlreadyExists) {
			reason = "login already exists"
		}

		response := protocol.RegisterFailure{
			Type:   protocol.AuthTypeRegisterFailure,
			Reason: reason,
		}

		marshaled, err := protocol.MarshalJSON(response)
		if err != nil {
			slog.Error("failed to marshal RegisterFailure", "err", err)
			return
		}

		if err := protocol.WriteFramedMessage(stream, marshaled); err != nil {
			slog.Error("failed to write RegisterFailure", "err", err)
		}

		return
	}

	// Регистрация успешна.
	response := protocol.RegisterSuccess{
		Type: protocol.AuthTypeRegisterSuccess,
	}

	marshaled, err := protocol.MarshalJSON(response)
	if err != nil {
		slog.Error("failed to marshal RegisterSuccess", "err", err)
		return
	}

	if err := protocol.WriteFramedMessage(stream, marshaled); err != nil {
		slog.Error("failed to write RegisterSuccess", "err", err)
		return
	}
}

func (a *AuthHandler) handleLogin(stream network.Stream, firstMsg []byte) {
	req, err := protocol.UnmarshalJSON[protocol.LoginRequest](firstMsg)
	if err != nil {
		slog.Error("failed to unmarshal LoginRequest", "err", err)
		return
	}

	user, err := db.GetUserByLogin(a.DB, req.Login)

	if errors.Is(err, sql.ErrNoRows) {
		response := protocol.LoginFailure{
			Type:   protocol.AuthTypeLoginFailure,
			Reason: "invalid login or password",
		}

		data, err := protocol.MarshalJSON(response)
		if err != nil {
			slog.Error("failed to marshal LoginFailure", "err", err)
			return
		}

		if err := protocol.WriteFramedMessage(stream, data); err != nil {
			slog.Error("failed to write LoginFailure", "err", err)
		}

		return
	}

	if err != nil {
		slog.Error("failed to get user by login", "err", err)

		response := protocol.LoginFailure{
			Type:   protocol.AuthTypeLoginFailure,
			Reason: "internal error",
		}

		data, err := protocol.MarshalJSON(response)
		if err != nil {
			slog.Error("failed to marshal LoginFailure", "err", err)
			return
		}

		if err := protocol.WriteFramedMessage(stream, data); err != nil {
			slog.Error("failed to write LoginFailure", "err", err)
		}

		return
	}

	// Пользователь найден.
	// Отправляем соли клиенту.
	response := protocol.LoginChallenge{
		Type:         protocol.AuthTypeLoginChallenge,
		PasswordSalt: user.PasswordSalt,
		LoginKeySalt: user.LoginKeySalt,
	}

	data, err := protocol.MarshalJSON(response)
	if err != nil {
		slog.Error("failed to marshal LoginChallenge", "err", err)
		return
	}

	if err := protocol.WriteFramedMessage(stream, data); err != nil {
		slog.Error("failed to write LoginChallenge", "err", err)
		return
	}

	// Ждём LoginVerify в том же stream.
	verifyData, err := protocol.ReadFramedMessage(stream)
	if err != nil {
		slog.Error("failed to read LoginVerify", "err", err)
		return
	}

	verify, err := protocol.UnmarshalJSON[protocol.LoginVerify](verifyData)
	if err != nil {
		slog.Error("failed to unmarshal LoginVerify", "err", err)
		return
	}

	// Проверяем пароль.
	if subtle.ConstantTimeCompare(
		verify.PasswordHash,
		user.PasswordHash,
	) != 1 {
		response := protocol.LoginFailure{
			Type:   protocol.AuthTypeLoginFailure,
			Reason: "invalid login or password",
		}

		data, err := protocol.MarshalJSON(response)
		if err != nil {
			slog.Error("failed to marshal LoginFailure", "err", err)
			return
		}

		if err := protocol.WriteFramedMessage(stream, data); err != nil {
			slog.Error("failed to write LoginFailure", "err", err)
		}

		return
	}

	// Пароль правильный.
	peerID := stream.Conn().RemotePeer().String()

	sessionID, err := db.CreateSession(
		a.DB,
		user.ID,
		peerID,
		24*time.Hour,
	)
	if err != nil {
		slog.Error("failed to create session", "err", err)

		response := protocol.LoginFailure{
			Type:   protocol.AuthTypeLoginFailure,
			Reason: "internal error",
		}

		data, err := protocol.MarshalJSON(response)
		if err != nil {
			slog.Error("failed to marshal LoginFailure", "err", err)
			return
		}

		if err := protocol.WriteFramedMessage(stream, data); err != nil {
			slog.Error("failed to write LoginFailure", "err", err)
		}

		return
	}

	// Успешный вход.
	Lresponse := protocol.LoginSuccess{
		Type:                 protocol.AuthTypeLoginSuccess,
		SessionID:            sessionID,
		FName:                user.FName,
		SName:                user.SName,
		EncryptedMasterKey:   user.EncryptedMasterKey,
		IdentityPublicKey:    user.IdentityPublicKey,
		EncryptedIdentityKey: user.EncryptedIdentityKey,
	}

	data, err = protocol.MarshalJSON(Lresponse)
	if err != nil {
		slog.Error("failed to marshal LoginSuccess", "err", err)
		return
	}

	if err := protocol.WriteFramedMessage(stream, data); err != nil {
		slog.Error("failed to write LoginSuccess", "err", err)
		return
	}
}
