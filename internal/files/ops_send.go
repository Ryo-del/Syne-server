package files

import (
	"errors"
	"os"
	"path/filepath"

	protocol "github.com/Ryo-del/Syne-protocol"
)

// maxReceivedBytes — потолок размера папки «Полученные» (переменная, чтобы
// тесты могли его уменьшить).
var maxReceivedBytes = int64(2) << 30

// doSend копирует файл в папку «Полученные» получателя (DestOwner). Нужно
// право send на файле (владельцу и преподавателю оно не нужно). Копия
// принадлежит получателю и занимает его квоту. В ответе — путь копии: по
// нему клиент отправляет в чат сообщение-вложение.
func (s *Service) doSend(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	src, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if src.info.IsDir() {
		return protocol.FilesResponse{}, invalid("only files can be sent")
	}
	if !canManage(actor, src.owner.Login) {
		if err := s.require(src, actor, protocol.ActionSend); err != nil {
			return protocol.FilesResponse{}, err
		}
	}
	rcpt, ok, err := s.st.UserByLogin(req.DestOwner)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if !ok {
		return protocol.FilesResponse{}, notFound()
	}
	if rcpt.Login == actor.Login {
		return protocol.FilesResponse{}, invalid("cannot send a file to yourself")
	}
	if blocked, err := s.st.IsBlocked(rcpt.Login, actor.Login); err != nil {
		return protocol.FilesResponse{}, err
	} else if blocked {
		// Получатель заблокировал отправителя: файл не доставляется, но
		// отправитель об этом не узнаёт (ответ как при успехе).
		name := Base(src.rel)
		return protocol.FilesResponse{Entry: &protocol.FileEntry{
			Name:      name,
			Path:      Join(ReceivedDir, name),
			Owner:     rcpt.Login,
			OwnerName: rcpt.DisplayName(),
		}}, nil
	}
	size := src.info.Size()

	unlock := s.lock(rcpt.Login)
	defer unlock()

	base, err := Resolve(s.root, rcpt.Login, "")
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return protocol.FilesResponse{}, err
	}
	inboxName := ReceivedDir
	if existing, taken, err := nameTaken(base, ReceivedDir); err != nil {
		return protocol.FilesResponse{}, err
	} else if taken {
		inboxName = existing // получатель мог создать папку с другим регистром
	}
	inboxAbs, err := Resolve(s.root, rcpt.Login, inboxName)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	switch info, err := os.Lstat(inboxAbs); {
	case errors.Is(err, os.ErrNotExist):
		if err := os.Mkdir(inboxAbs, 0o755); err != nil {
			return protocol.FilesResponse{}, err
		}
	case err != nil:
		return protocol.FilesResponse{}, err
	case !okKind(info) || !info.IsDir():
		return protocol.FilesResponse{}, invalid("the recipient cannot receive files right now")
	}

	if treeSize(inboxAbs)+size > maxReceivedBytes {
		return protocol.FilesResponse{}, newErr(protocol.FilesErrQuota, "the recipient's inbox is full")
	}
	if err := s.reserve(rcpt, size); err != nil {
		return protocol.FilesResponse{}, err
	}

	names, err := dirNames(inboxAbs)
	if err != nil {
		s.release(rcpt.Login, size)
		return protocol.FilesResponse{}, err
	}
	folds := make(map[string]bool, len(names))
	for _, n := range names {
		folds[Fold(n)] = true
	}
	name := Base(src.rel)
	if folds[Fold(name)] {
		name = uniqueName(folds, name, false, false)
	}
	dst := filepath.Join(inboxAbs, name)
	copied, err := copyFile(src.abs, dst)
	if err != nil {
		s.release(rcpt.Login, size)
		return protocol.FilesResponse{}, err
	}
	switch {
	case copied < size:
		s.release(rcpt.Login, size-copied)
	case copied > size: // файл вырос во время копирования
		if err := s.reserve(rcpt, copied-size); err != nil {
			_ = os.Remove(dst)
			s.release(rcpt.Login, size)
			return protocol.FilesResponse{}, err
		}
	}

	info, err := os.Lstat(dst)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	eng, err := s.engineFor(rcpt.Login)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	asRecipient := Actor{Login: rcpt.Login, Role: rcpt.Role}
	e, err := s.entryFor(eng, asRecipient, rcpt, Join(inboxName, name), info)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{Entry: &e}, nil
}
