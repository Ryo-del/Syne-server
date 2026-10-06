package files

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	protocol "github.com/Ryo-del/Syne-protocol"
)

// ---------- чтение ----------

// OpenRead открывает файл для передачи клиенту. forDownload — обычное
// скачивание (право download, любой тип файла); иначе открытие в редакторе
// (право view, только .txt до MaxEditBytes). Вызывающий закрывает файл и
// отправляет ровно entry.Size байт.
func (s *Service) OpenRead(actor Actor, req protocol.FilesRequest, forDownload bool) (*os.File, protocol.FileEntry, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return nil, protocol.FileEntry{}, err
	}
	if t.info.IsDir() {
		return nil, protocol.FileEntry{}, invalid("not a file")
	}
	need := protocol.ActionView
	if forDownload {
		need = protocol.ActionDownload
	}
	if err := s.require(t, actor, need); err != nil {
		return nil, protocol.FileEntry{}, err
	}
	if !forDownload {
		if !EditableName(Base(t.rel)) {
			return nil, protocol.FileEntry{}, invalid("only .txt files can be opened")
		}
		if t.info.Size() > MaxEditBytes {
			return nil, protocol.FileEntry{}, invalid("file is too large to open (max 10 MiB)")
		}
	}

	f, err := os.Open(t.abs)
	if err != nil {
		return nil, protocol.FileEntry{}, err
	}
	// Между проверкой и открытием путь могли подменить ссылкой: открытый файл
	// должен быть тем же самым, что лежит по этому пути.
	fi, err := f.Stat()
	li, lerr := os.Lstat(t.abs)
	if err != nil || lerr != nil || !fi.Mode().IsRegular() || !os.SameFile(fi, li) {
		_ = f.Close()
		return nil, protocol.FileEntry{}, notFound()
	}
	if !forDownload && fi.Size() > MaxEditBytes {
		_ = f.Close()
		return nil, protocol.FileEntry{}, invalid("file is too large to open (max 10 MiB)")
	}
	entry, err := s.entryFor(t.eng, actor, t.owner, t.rel, fi)
	if err != nil {
		_ = f.Close()
		return nil, protocol.FileEntry{}, err
	}
	return f, entry, nil
}

// ---------- загрузка и сохранение ----------

// Upload — принимаемый файл. Байты пишутся во временный файл рядом с целью и
// только после полного получения переименовываются на место: незавершённая
// загрузка никогда не портит существующий файл.
type Upload struct {
	s        *Service
	actor    Actor
	owner    User
	dirAbs   string // папка назначения
	dirRel   string
	name     string // итоговое имя
	size     int64  // сколько байт обещал клиент
	reserved int64  // сколько места зарезервировано в квоте владельца
	replace  bool   // заменяется существующий файл
	check    bool   // write: сверять время изменения
	baseMod  int64
	f        *os.File
	tmp      string
	done     bool
}

func createTemp(dir string) (*os.File, string, error) {
	for i := 0; i < 5; i++ {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, "", err
		}
		p := filepath.Join(dir, TempPrefix+hex.EncodeToString(b[:]))
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, p, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("files: cannot create a temp file")
}

// startUpload резервирует место (только прирост относительно заменяемого
// файла) и создаёт временный файл.
func (s *Service) startUpload(up *Upload, oldSize int64) (*Upload, error) {
	up.reserved = max(0, up.size-oldSize)
	if up.reserved > 0 {
		if err := s.reserve(up.owner, up.reserved); err != nil {
			up.reserved = 0
			return nil, err
		}
	}
	f, tmp, err := createTemp(up.dirAbs)
	if err != nil {
		s.release(up.owner.Login, up.reserved)
		return nil, err
	}
	up.f, up.tmp = f, tmp
	return up, nil
}

// BeginUpload проверяет права, имя, конфликт и квоту. Результат (nil, nil)
// означает политику skip: принимать нечего.
func (s *Service) BeginUpload(actor Actor, req protocol.FilesRequest) (*Upload, error) {
	parent, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return nil, err
	}
	if !parent.info.IsDir() {
		return nil, invalid("parent is not a folder")
	}
	if err := s.require(parent, actor, protocol.ActionPaste); err != nil {
		return nil, err
	}
	name, err := CleanName(req.Name)
	if err != nil {
		return nil, err
	}
	if req.Size < 0 || req.Size > MaxFileBytes {
		return nil, invalid("file is too large (max 2 GiB)")
	}

	names, err := dirNames(parent.abs)
	if err != nil {
		return nil, err
	}
	folds := make(map[string]bool, len(names))
	existingName := ""
	for _, n := range names {
		folds[Fold(n)] = true
		if Fold(n) == Fold(name) {
			existingName = n
		}
	}

	up := &Upload{
		s:      s,
		actor:  actor,
		owner:  parent.owner,
		dirAbs: parent.abs,
		dirRel: parent.rel,
		name:   name,
		size:   req.Size,
	}
	var oldSize int64
	if existingName != "" {
		switch req.OnConflict {
		case "":
			return nil, newErr(protocol.FilesErrExists, "an item named "+existingName+" already exists")
		case protocol.ConflictSkip:
			return nil, nil
		case protocol.ConflictRename:
			up.name = uniqueName(folds, name, false, false)
		case protocol.ConflictReplace:
			exInfo, err := os.Lstat(filepath.Join(parent.abs, existingName))
			if err != nil || !okKind(exInfo) {
				return nil, notFound()
			}
			if exInfo.IsDir() {
				return nil, invalid("cannot replace a folder with a file")
			}
			if !canManage(actor, parent.owner.Login) {
				ok, err := parent.eng.Allowed(actor, parent.owner.Login, Join(parent.rel, existingName), protocol.ActionDelete)
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, forbidden()
				}
			}
			up.replace = true
			up.name = existingName
			oldSize = exInfo.Size()
		default:
			return nil, invalid("unknown conflict policy")
		}
	}
	return s.startUpload(up, oldSize)
}

// BeginWrite — сохранение изменённого .txt. Нужно право edit. Если
// BaseModTime задан и файл с тех пор менялся, возвращается ошибка conflict.
func (s *Service) BeginWrite(actor Actor, req protocol.FilesRequest) (*Upload, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return nil, err
	}
	if t.info.IsDir() || !EditableName(Base(t.rel)) {
		return nil, invalid("only .txt files can be edited")
	}
	if err := s.require(t, actor, protocol.ActionEdit); err != nil {
		return nil, err
	}
	if req.Size < 0 || req.Size > MaxEditBytes {
		return nil, invalid("file is too large to save (max 10 MiB)")
	}
	if req.BaseModTime != 0 && t.info.ModTime().UnixMilli() != req.BaseModTime {
		return nil, newErr(protocol.FilesErrConflict, "the file was changed by someone else")
	}
	up := &Upload{
		s:       s,
		actor:   actor,
		owner:   t.owner,
		dirAbs:  filepath.Dir(t.abs),
		dirRel:  Parent(t.rel),
		name:    Base(t.rel),
		size:    req.Size,
		replace: true,
		check:   req.BaseModTime != 0,
		baseMod: req.BaseModTime,
	}
	return s.startUpload(up, t.info.Size())
}

// Commit принимает ровно size байт из r и атомарно ставит файл на место.
// Любая ошибка возвращает место в квоту и удаляет временный файл.
func (u *Upload) Commit(r io.Reader) (protocol.FilesResponse, error) {
	defer u.Abort()
	if u.done || u.f == nil {
		return protocol.FilesResponse{}, errors.New("files: upload is already finished")
	}

	if _, err := io.CopyN(u.f, r, u.size); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return protocol.FilesResponse{}, invalid("upload is shorter than declared")
		}
		return protocol.FilesResponse{}, err
	}
	if err := u.f.Sync(); err != nil {
		return protocol.FilesResponse{}, err
	}
	err := u.f.Close()
	u.f = nil
	if err != nil {
		return protocol.FilesResponse{}, err
	}

	unlock := u.s.lock(u.owner.Login)
	defer unlock()

	finalAbs := filepath.Join(u.dirAbs, u.name)
	var curOld int64
	if u.replace {
		cur, err := os.Lstat(finalAbs)
		switch {
		case err == nil:
			if !okKind(cur) || cur.IsDir() {
				return protocol.FilesResponse{}, invalid("target is not a file")
			}
			if u.check && cur.ModTime().UnixMilli() != u.baseMod {
				return protocol.FilesResponse{}, newErr(protocol.FilesErrConflict, "the file was changed by someone else")
			}
			curOld = cur.Size()
		case errors.Is(err, os.ErrNotExist):
			if u.check {
				return protocol.FilesResponse{}, notFound() // файл удалили, пока его правили
			}
			// загрузка с заменой: файл пропал, ведём себя как при новой загрузке
		default:
			return protocol.FilesResponse{}, err
		}
	} else {
		existing, taken, err := nameTaken(u.dirAbs, u.name)
		if err != nil {
			return protocol.FilesResponse{}, err
		}
		if taken {
			return protocol.FilesResponse{}, newErr(protocol.FilesErrExists, "an item named "+existing+" already exists")
		}
	}

	// Если за время загрузки заменяемый файл уменьшился, нужно доразместить место.
	need := u.size - curOld
	if need > u.reserved {
		if err := u.s.reserve(u.owner, need-u.reserved); err != nil {
			return protocol.FilesResponse{}, err
		}
		u.reserved = need
	}

	if err := os.Rename(u.tmp, finalAbs); err != nil {
		return protocol.FilesResponse{}, err
	}
	u.done = true
	u.tmp = ""
	if u.reserved > need {
		u.s.release(u.owner.Login, u.reserved-need)
	}

	// Файл уже на месте; ошибки ниже не должны превращать успех в неудачу.
	info, err := os.Lstat(finalAbs)
	if err != nil {
		slog.Error("files: stat after upload", "err", err)
		return protocol.FilesResponse{}, nil
	}
	eng, err := u.s.engineFor(u.owner.Login)
	if err != nil {
		slog.Error("files: rules after upload", "err", err)
		return protocol.FilesResponse{}, nil
	}
	e, err := u.s.entryFor(eng, u.actor, u.owner, Join(u.dirRel, u.name), info)
	if err != nil {
		slog.Error("files: entry after upload", "err", err)
		return protocol.FilesResponse{}, nil
	}
	return protocol.FilesResponse{Entry: &e}, nil
}

// Abort отменяет загрузку: закрывает и удаляет временный файл, возвращает
// зарезервированное место. Безопасно вызывать повторно и после Commit.
func (u *Upload) Abort() {
	if u == nil || u.done {
		return
	}
	u.done = true
	if u.f != nil {
		_ = u.f.Close()
		u.f = nil
	}
	if u.tmp != "" {
		_ = os.Remove(u.tmp)
		u.tmp = ""
	}
	u.s.release(u.owner.Login, u.reserved)
	u.reserved = 0
}

// ---------- уборка ----------

// SweepTemps удаляет временные файлы загрузок, оставшиеся после сбоя
// (старше двух часов), и сбрасывает учёт места затронутых владельцев: он
// пересчитается обходом папки. Запускать в фоне при старте сервера.
func (s *Service) SweepTemps() {
	cutoff := time.Now().Add(-2 * time.Hour)
	touched := map[string]bool{}
	_ = filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasPrefix(d.Name(), TempPrefix) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().After(cutoff) {
			return nil
		}
		if os.Remove(p) == nil {
			if rel, err := filepath.Rel(s.root, p); err == nil {
				touched[strings.Split(rel, string(filepath.Separator))[0]] = true
			}
		}
		return nil
	})
	for dir := range touched {
		login, ok := loginFromDir(dir)
		if !ok {
			continue
		}
		if err := s.st.ForgetUsage(login); err != nil {
			slog.Error("files: reset usage after sweep", "owner", login, "err", err)
		}
	}
}

// loginFromDir — обратное к OwnerDir: имя папки на диске -> логин.
func loginFromDir(dir string) (string, bool) {
	if strings.HasPrefix(dir, "_") {
		b, err := hex.DecodeString(dir[1:])
		return string(b), err == nil
	}
	return dir, dir != ""
}
