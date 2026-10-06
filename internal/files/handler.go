package files

import (
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"time"

	"server/internal/db"

	protocol "github.com/Ryo-del/Syne-protocol"
	"github.com/libp2p/go-libp2p/core/network"
)

const maxRequestFrame = 1 << 20

var errUnauthorized = errors.New("unauthorized")

// Handler — libp2p-обработчик протокола файлов: кадр запроса, проверка
// сессии (как в vault), выполнение, кадр ответа. Для read/download после
// ответа идут байты файла, для upload/write после ready — байты от клиента.
type Handler struct {
	conn *sql.DB
	svc  *Service
}

func NewHandler(database *sql.DB, svc *Service) *Handler {
	return &Handler{conn: database, svc: svc}
}

func (h *Handler) HandleStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(30 * time.Second))

	data, err := protocol.ReadFramedMessageMax(stream, maxRequestFrame)
	if err != nil {
		slog.Error("files: failed to read request", "err", err)
		return
	}
	req, err := protocol.UnmarshalJSON[protocol.FilesRequest](data)
	if err != nil {
		_ = h.reply(stream, protocol.FilesResponse{Code: protocol.FilesErrInvalidRequest, Error: "invalid request"})
		return
	}

	actor, err := h.authenticate(req.SessionID, stream.Conn().RemotePeer().String())
	if err != nil {
		_ = h.reply(stream, protocol.FilesResponse{Code: protocol.FilesErrUnauthorized, Error: "unauthorized"})
		return
	}

	_ = stream.SetDeadline(time.Now().Add(opTimeout(req.Op)))
	switch req.Op {
	case protocol.FilesOpRead, protocol.FilesOpDownload:
		h.serveRead(stream, actor, req)
	case protocol.FilesOpUpload, protocol.FilesOpWrite:
		h.serveUpload(stream, actor, req)
	case protocol.FilesOpDownloadDir:
		h.serveDirZip(stream, actor, req)
	default:
		_ = h.reply(stream, h.svc.Do(actor, req))
	}
}

// serveRead: кадр {ok, entry}, затем ровно entry.Size байт.
func (h *Handler) serveRead(stream network.Stream, actor Actor, req protocol.FilesRequest) {
	f, entry, err := h.svc.OpenRead(actor, req, req.Op == protocol.FilesOpDownload)
	if err != nil {
		_ = h.reply(stream, respErr(err))
		return
	}
	defer f.Close()
	if err := h.reply(stream, protocol.FilesResponse{OK: true, Entry: &entry}); err != nil {
		return
	}
	if _, err := io.CopyN(stream, f, entry.Size); err != nil {
		slog.Error("files: download interrupted", "err", err)
		_ = stream.Reset() // клиент увидит обрыв, а не «короткий» успешный файл
	}
}

// serveDirZip: кадр {ok, entry, skipped}, затем zip-поток до конца stream.
func (h *Handler) serveDirZip(stream network.Stream, actor Actor, req protocol.FilesRequest) {
	job, entry, err := h.svc.PrepareDirZip(actor, req)
	if err != nil {
		_ = h.reply(stream, respErr(err))
		return
	}
	if err := h.reply(stream, protocol.FilesResponse{OK: true, Entry: &entry, Skipped: job.Skipped}); err != nil {
		return
	}
	if err := job.WriteZip(stream); err != nil {
		slog.Error("files: zip interrupted", "err", err)
		_ = stream.Reset() // клиент увидит обрыв, а не «целый» архив
	}
}

// serveUpload: кадр {ok, ready}, затем клиент шлёт ровно size байт,
// затем итоговый кадр {ok, entry}. Политика skip: сразу {ok, skipped}.
func (h *Handler) serveUpload(stream network.Stream, actor Actor, req protocol.FilesRequest) {
	var (
		up  *Upload
		err error
	)
	if req.Op == protocol.FilesOpWrite {
		up, err = h.svc.BeginWrite(actor, req)
	} else {
		up, err = h.svc.BeginUpload(actor, req)
	}
	if err != nil {
		_ = h.reply(stream, respErr(err))
		return
	}
	if up == nil {
		_ = h.reply(stream, protocol.FilesResponse{OK: true, Skipped: 1})
		return
	}
	defer up.Abort()

	if err := h.reply(stream, protocol.FilesResponse{OK: true, Ready: true}); err != nil {
		return
	}
	final, err := up.Commit(stream)
	if err != nil {
		_ = h.reply(stream, respErr(err))
		return
	}
	final.OK = true
	_ = h.reply(stream, final)
}

// authenticate: сессия существует, не истекла и принадлежит устройству,
// выдавшему её при входе. Роль берётся из БД, а не из запроса.
func (h *Handler) authenticate(sessionID, remotePeer string) (Actor, error) {
	info, err := db.GetSessionInfo(h.conn, sessionID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("files: session lookup failed", "err", err)
		}
		return Actor{}, errUnauthorized
	}
	if time.Now().Unix() > info.ExpiresAt || info.PeerID != remotePeer {
		return Actor{}, errUnauthorized
	}
	u, ok, err := h.svc.st.UserByLogin(info.Login)
	if err != nil {
		slog.Error("files: user lookup failed", "err", err)
		return Actor{}, errUnauthorized
	}
	if !ok {
		return Actor{}, errUnauthorized
	}
	return Actor{Login: u.Login, Role: u.Role}, nil
}

func (h *Handler) reply(stream network.Stream, resp protocol.FilesResponse) error {
	data, err := protocol.MarshalJSON(resp)
	if err != nil {
		slog.Error("files: failed to marshal response", "err", err)
		return err
	}
	if uint32(len(data)) > protocol.FilesMaxFrame {
		data, _ = protocol.MarshalJSON(protocol.FilesResponse{
			Code: protocol.FilesErrInternal, Error: "response too large",
		})
	}
	if err := protocol.WriteFramedMessage(stream, data); err != nil {
		slog.Error("files: failed to write response", "err", err)
		return err
	}
	return nil
}

func opTimeout(op string) time.Duration {
	switch op {
	case protocol.FilesOpPaste, protocol.FilesOpDuplicate, protocol.FilesOpDelete, protocol.FilesOpSend:
		return 15 * time.Minute
	case protocol.FilesOpRead, protocol.FilesOpDownload, protocol.FilesOpDownloadDir,
		protocol.FilesOpUpload, protocol.FilesOpWrite:
		return time.Hour
	}
	return 30 * time.Second
}
