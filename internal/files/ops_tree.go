package files

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	protocol "github.com/Ryo-del/Syne-protocol"
)

func invalid(msg string) *Error { return newErr(protocol.FilesErrInvalidRequest, msg) }

// ---------- list / stat ----------

// listOwners — папки других пользователей («Сервер»). Преподаватель видит всех,
// остальные только тех, у кого для них что-то открыто.
func (s *Service) listOwners(actor Actor) (protocol.FilesResponse, error) {
	users, err := s.st.AllUsers()
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	var candidates map[string]bool
	if !actor.IsTeacher() {
		logins, err := s.st.OwnersWithVisibleRules()
		if err != nil {
			return protocol.FilesResponse{}, err
		}
		candidates = make(map[string]bool, len(logins))
		for _, l := range logins {
			candidates[l] = true
		}
	}

	var out []protocol.FileEntry
	for _, u := range users {
		if u.Login == actor.Login {
			continue
		}
		if candidates != nil && !candidates[u.Login] {
			continue
		}
		eng, err := s.engineFor(u.Login)
		if err != nil {
			return protocol.FilesResponse{}, err
		}
		if !actor.IsTeacher() {
			vis, err := eng.Visible(actor, u.Login, "", true)
			if err != nil {
				return protocol.FilesResponse{}, err
			}
			if !vis {
				continue
			}
		}
		e, err := s.rootEntry(eng, actor, u)
		if err != nil {
			return protocol.FilesResponse{}, err
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Owner < out[j].Owner
	})
	return protocol.FilesResponse{Entries: out}, nil
}

func (s *Service) doList(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if !t.info.IsDir() {
		return protocol.FilesResponse{}, invalid("not a folder")
	}
	des, err := os.ReadDir(t.abs)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	priv := canManage(actor, t.owner.Login)

	var out []protocol.FileEntry
	truncated := false
	for _, de := range des {
		name := de.Name()
		if strings.HasPrefix(name, TempPrefix) {
			continue
		}
		if IsJunkName(name) {
			continue
		}
		// Имена, созданные в обход API (недопустимые символы, не NFC), адресовать нельзя.
		if c, err := CleanName(name); err != nil || c != name {
			continue
		}
		info, err := de.Info()
		if err != nil || !okKind(info) {
			continue
		}
		rel := Join(t.rel, name)
		if !priv {
			vis, err := t.eng.Visible(actor, t.owner.Login, rel, info.IsDir())
			if err != nil {
				return protocol.FilesResponse{}, err
			}
			if !vis {
				continue
			}
		}
		if len(out) >= maxListEntries {
			truncated = true
			break
		}
		e, err := s.entryFor(t.eng, actor, t.owner, rel, info)
		if err != nil {
			return protocol.FilesResponse{}, err
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Name < out[j].Name
	})
	return protocol.FilesResponse{Entries: out, Truncated: truncated}, nil
}

func (s *Service) doStat(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	e, err := s.entryFor(t.eng, actor, t.owner, t.rel, t.info)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{Entry: &e}, nil
}

// ---------- mkdir / create ----------

// createChild создаёт папку или пустой файл. Нужно право paste в родительской папке.
func (s *Service) createChild(actor Actor, req protocol.FilesRequest, isDir bool) (protocol.FilesResponse, error) {
	parent, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if !parent.info.IsDir() {
		return protocol.FilesResponse{}, invalid("parent is not a folder")
	}
	if err := s.require(parent, actor, protocol.ActionPaste); err != nil {
		return protocol.FilesResponse{}, err
	}
	name, err := CleanName(req.Name)
	if err != nil {
		return protocol.FilesResponse{}, err
	}

	unlock := s.lock(parent.owner.Login)
	defer unlock()

	if existing, taken, err := nameTaken(parent.abs, name); err != nil {
		return protocol.FilesResponse{}, err
	} else if taken {
		return protocol.FilesResponse{}, newErr(protocol.FilesErrExists, "an item named "+existing+" already exists")
	}
	abs := filepath.Join(parent.abs, name)
	if isDir {
		err = os.Mkdir(abs, 0o755)
	} else {
		var f *os.File
		f, err = os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			err = f.Close()
		}
	}
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	e, err := s.entryFor(parent.eng, actor, parent.owner, Join(parent.rel, name), info)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{Entry: &e}, nil
}

// ---------- rename ----------

// doRename требует права edit. Файлы чужих владельцев можно переименовать
// только из .txt в .txt: редактировать другие типы нельзя.
func (s *Service) doRename(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if t.rel == "" {
		return protocol.FilesResponse{}, invalid("cannot rename the root")
	}
	newName, err := CleanName(req.Name)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	oldName := Base(t.rel)

	if !canManage(actor, t.owner.Login) {
		if err := s.require(t, actor, protocol.ActionEdit); err != nil {
			return protocol.FilesResponse{}, err
		}
		if !t.info.IsDir() && !(EditableName(oldName) && EditableName(newName)) {
			return protocol.FilesResponse{}, forbidden()
		}
	}

	unlock := s.lock(t.owner.Login)
	defer unlock()

	parentAbs := filepath.Dir(t.abs)
	newRel := Join(Parent(t.rel), newName)
	newAbs := filepath.Join(parentAbs, newName)

	if newName != oldName {
		if Fold(newName) != Fold(oldName) {
			if existing, taken, err := nameTaken(parentAbs, newName); err != nil {
				return protocol.FilesResponse{}, err
			} else if taken {
				return protocol.FilesResponse{}, newErr(protocol.FilesErrExists, "an item named "+existing+" already exists")
			}
		}
		if err := os.Rename(t.abs, newAbs); err != nil {
			return protocol.FilesResponse{}, err
		}
		oldKey, newKey := Fold(t.rel), Fold(newRel)
		if oldKey != newKey {
			if err := s.st.RenamePrefix(t.owner.Login, oldKey, newKey); err != nil {
				if rbErr := os.Rename(newAbs, t.abs); rbErr != nil {
					slog.Error("files: rename rollback failed", "err", rbErr)
				}
				return protocol.FilesResponse{}, err
			}
			if err := s.st.RenameFavoritesPrefix(t.owner.Login, oldKey, newKey); err != nil {
				slog.Error("files: rename favorites", "err", err)
			}
		}
	}

	info, err := os.Lstat(newAbs)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	eng, err := s.engineFor(t.owner.Login) // правила уже перенесены
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	e, err := s.entryFor(eng, actor, t.owner, newRel, info)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{Entry: &e}, nil
}

// ---------- delete ----------

// deleteNoLock удаляет элемент (вызывающий держит lock владельца). Для чужих
// папок нужно право delete на самом элементе и на каждом вложенном (видимом
// и невидимом): частичное удаление не выполняется.
func (s *Service) deleteNoLock(actor Actor, t *target) error {
	if t.rel == "" {
		return invalid("cannot delete the root")
	}
	if !canManage(actor, t.owner.Login) {
		if err := s.require(t, actor, protocol.ActionDelete); err != nil {
			return err
		}
		if t.info.IsDir() {
			need := []protocol.Action{protocol.ActionDelete}
			if _, _, err := s.gather(actor, t.eng, t.owner.Login, false, t.rel, t.abs, t.info, need, true); err != nil {
				return err
			}
		}
	}
	size := treeSize(t.abs)
	if err := os.RemoveAll(t.abs); err != nil {
		return err
	}
	key := Fold(t.rel)
	if err := s.st.DeletePrefix(t.owner.Login, key); err != nil {
		slog.Error("files: delete rules", "err", err)
	}
	if err := s.st.DeleteFavoritesPrefix(t.owner.Login, key); err != nil {
		slog.Error("files: delete favorites", "err", err)
	}
	s.release(t.owner.Login, size)
	return nil
}

func (s *Service) doDelete(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	unlock := s.lock(t.owner.Login)
	defer unlock()
	if err := s.deleteNoLock(actor, t); err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{}, nil
}

// ---------- copy / paste / duplicate ----------

func (s *Service) doPaste(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	src, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	dst, err := s.open(actor, req.DestOwner, req.DestPath)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return s.copyInto(actor, src, dst, req.OnConflict, false)
}

// doDuplicate — копия рядом с оригиналом: «имя (копия).txt», «имя (копия 2).txt».
// Нужны права copy на элементе и paste в его папке.
func (s *Service) doDuplicate(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	src, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if src.rel == "" {
		return protocol.FilesResponse{}, invalid("cannot duplicate the root")
	}
	dst, err := s.open(actor, req.Owner, Parent(src.rel))
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return s.copyInto(actor, src, dst, "", true)
}

// copyInto копирует src в папку dst. Для чужих деревьев копируется только то,
// что пользователь видит и может копировать (число пропущенных — Skipped).
// Права на копию не переносятся: она наследует права папки назначения.
func (s *Service) copyInto(actor Actor, src, dst *target, policy string, dup bool) (protocol.FilesResponse, error) {
	if src.rel == "" {
		return protocol.FilesResponse{}, invalid("cannot copy a folder root")
	}
	if !dst.info.IsDir() {
		return protocol.FilesResponse{}, invalid("destination is not a folder")
	}
	srcPriv := canManage(actor, src.owner.Login)
	dstPriv := canManage(actor, dst.owner.Login)
	if !srcPriv {
		if err := s.require(src, actor, protocol.ActionCopy); err != nil {
			return protocol.FilesResponse{}, err
		}
	}
	if !dstPriv {
		if err := s.require(dst, actor, protocol.ActionPaste); err != nil {
			return protocol.FilesResponse{}, err
		}
	}
	if src.owner.Login == dst.owner.Login {
		sk, dk := Fold(src.rel), Fold(dst.rel)
		if dk == sk || strings.HasPrefix(dk+"/", sk+"/") {
			return protocol.FilesResponse{}, invalid("cannot copy a folder into itself")
		}
	}

	need := []protocol.Action{protocol.ActionView, protocol.ActionCopy}
	nodes, skipped, err := s.gather(actor, src.eng, src.owner.Login, srcPriv, src.rel, src.abs, src.info, need, false)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	var total int64
	for _, n := range nodes {
		total += n.size
	}

	unlock := s.lock(dst.owner.Login)
	defer unlock()

	names, err := dirNames(dst.abs)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	folds := make(map[string]bool, len(names))
	base := Base(src.rel)
	existingName := ""
	for _, n := range names {
		folds[Fold(n)] = true
		if Fold(n) == Fold(base) {
			existingName = n
		}
	}

	name := base
	replace := false
	switch {
	case dup:
		name = uniqueName(folds, base, src.info.IsDir(), true)
	case existingName != "":
		switch policy {
		case "":
			return protocol.FilesResponse{}, newErr(protocol.FilesErrExists, "an item named "+existingName+" already exists")
		case protocol.ConflictSkip:
			return protocol.FilesResponse{Skipped: 1}, nil
		case protocol.ConflictRename:
			name = uniqueName(folds, base, src.info.IsDir(), false)
		case protocol.ConflictReplace:
			replace = true
		default:
			return protocol.FilesResponse{}, invalid("unknown conflict policy")
		}
	}

	var existing *target
	if replace {
		exAbs := filepath.Join(dst.abs, existingName)
		exInfo, err := os.Lstat(exAbs)
		if err != nil || !okKind(exInfo) {
			return protocol.FilesResponse{}, notFound()
		}
		exRel := Join(dst.rel, existingName)
		if src.owner.Login == dst.owner.Login && Fold(exRel) == Fold(src.rel) {
			return protocol.FilesResponse{}, invalid("source and destination are the same")
		}
		existing = &target{owner: dst.owner, rel: exRel, abs: exAbs, info: exInfo, eng: dst.eng}
	}

	// Место резервируется до копирования; при замене существующий элемент
	// освобождает своё место только после удаления.
	if err := s.reserve(dst.owner, total); err != nil {
		return protocol.FilesResponse{}, err
	}
	reserved := total
	fail := func(err error) (protocol.FilesResponse, error) {
		s.release(dst.owner.Login, reserved)
		return protocol.FilesResponse{}, err
	}
	if existing != nil {
		if err := s.deleteNoLock(actor, existing); err != nil {
			return fail(err)
		}
	}

	newRel := Join(dst.rel, name)
	newAbs := filepath.Join(dst.abs, name)
	createdRoot := false
	cleanup := func() {
		if createdRoot {
			_ = os.RemoveAll(newAbs)
		}
	}
	var copied int64
	for i, n := range nodes {
		target := newAbs + filepath.FromSlash(n.rel[len(src.rel):])
		var err error
		if n.isDir {
			err = os.Mkdir(target, 0o755)
		} else {
			var w int64
			w, err = copyFile(n.abs, target)
			copied += w
		}
		if err != nil {
			cleanup()
			return fail(err)
		}
		if i == 0 {
			createdRoot = true
		}
	}

	switch {
	case copied < reserved:
		s.release(dst.owner.Login, reserved-copied)
	case copied > reserved: // файл вырос во время копирования
		if err := s.reserve(dst.owner, copied-reserved); err != nil {
			cleanup()
			return fail(err)
		}
	}

	info, err := os.Lstat(newAbs)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	eng, err := s.engineFor(dst.owner.Login)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	e, err := s.entryFor(eng, actor, dst.owner, newRel, info)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{Entry: &e, Skipped: skipped}, nil
}

// copyFile копирует обычный файл в новый (O_EXCL: существующий не затрагивается).
// При ошибке копирования недописанный файл удаляется.
func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return n, err
	}
	if st, err := in.Stat(); err == nil {
		_ = os.Chtimes(dst, st.ModTime(), st.ModTime())
	}
	return n, nil
}

// uniqueName подбирает свободное имя: «имя (1).txt» или, для duplicate,
// «имя (копия).txt», «имя (копия 2).txt». folds — занятые имена в форме Fold.
func uniqueName(folds map[string]bool, base string, isDir, dup bool) string {
	stem, ext := base, ""
	if !isDir {
		if i := strings.LastIndexByte(base, '.'); i > 0 {
			stem, ext = base[:i], base[i:]
		}
	}
	for i := 1; ; i++ {
		var suffix string
		switch {
		case dup && i == 1:
			suffix = " (копия)"
		case dup:
			suffix = fmt.Sprintf(" (копия %d)", i)
		default:
			suffix = fmt.Sprintf(" (%d)", i)
		}
		cand := trimToBytes(stem, maxNameBytes-len(suffix)-len(ext)) + suffix + ext
		if !folds[Fold(cand)] {
			return cand
		}
	}
}

func trimToBytes(s string, n int) string {
	if n < 0 {
		n = 0
	}
	for len(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// Package files — ядро файлового менеджера: безопасные пути, права (ACL),
// квоты. Файлы лежат на диске как обычные папки:
//
//	<files_path>/<папка владельца>/<относительный путь>
//
// Папка владельца выводится из логина (OwnerDir). Все пути, приходящие от
// клиента, проходят CleanRel, а перед обращением к диску — Resolve.
