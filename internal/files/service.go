package files

import (
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	protocol "github.com/Ryo-del/Syne-protocol"
)

const (
	// MaxFileBytes — потолок размера одного файла (загрузка, этап 2б).
	MaxFileBytes = int64(2) << 30
	// MaxEditBytes — потолок размера файла для открытия и сохранения в редакторе (этап 2б).
	MaxEditBytes = int64(10) << 20
	// ReceivedDir — папка, куда попадают файлы, отправленные в чате (этап 2б).
	ReceivedDir = "Полученные"

	maxListEntries = 10000
)

// Error — ошибка с кодом протокола; её текст безопасно показывать клиенту.
type Error struct{ Code, Msg string }

func (e *Error) Error() string { return e.Msg }

func newErr(code, msg string) *Error { return &Error{Code: code, Msg: msg} }

func notFound() *Error  { return newErr(protocol.FilesErrNotFound, "not found") }
func forbidden() *Error { return newErr(protocol.FilesErrForbidden, "forbidden") }

// respErr переводит ошибку в ответ. Неизвестные ошибки (в них могут быть
// пути сервера) клиенту не показываются: пишутся в лог.
func respErr(err error) protocol.FilesResponse {
	var fe *Error
	switch {
	case errors.As(err, &fe):
		return protocol.FilesResponse{Code: fe.Code, Error: fe.Msg}
	case errors.Is(err, ErrForbidden):
		return protocol.FilesResponse{Code: protocol.FilesErrForbidden, Error: "forbidden"}
	case errors.Is(err, ErrInvalidPath), errors.Is(err, ErrInvalidName):
		return protocol.FilesResponse{Code: protocol.FilesErrInvalidPath, Error: err.Error()}
	case errors.Is(err, ErrInvalidRule), errors.Is(err, errBadUIState):
		return protocol.FilesResponse{Code: protocol.FilesErrInvalidRequest, Error: err.Error()}
	case errors.Is(err, ErrQuotaExceeded):
		return protocol.FilesResponse{Code: protocol.FilesErrQuota, Error: "not enough space"}
	case errors.Is(err, os.ErrNotExist):
		return protocol.FilesResponse{Code: protocol.FilesErrNotFound, Error: "not found"}
	case errors.Is(err, os.ErrExist):
		return protocol.FilesResponse{Code: protocol.FilesErrExists, Error: "already exists"}
	}
	slog.Error("files: internal error", "err", err)
	return protocol.FilesResponse{Code: protocol.FilesErrInternal, Error: "internal error"}
}

// Service — файловые операции с проверкой прав и квот.
type Service struct {
	root    string
	st      *SQLStore
	eng     *Engine // поверх SQL-хранилища: изменение и описание правил
	perUser func() int64
	locks   sync.Map // логин владельца -> *sync.Mutex
}

// NewService: root — каталог files_path из конфига, perUser — квота на ученика
// в байтах (читается при каждой проверке, поэтому изменение настроек действует сразу).
func NewService(database *sql.DB, root string, perUser func() int64) (*Service, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("files: files_path is empty")
	}
	if perUser == nil {
		perUser = func() int64 { return Unlimited }
	}
	st := NewSQLStore(database)
	return &Service{root: root, st: st, eng: NewEngine(st), perUser: perUser}, nil
}

// Store нужен обработчику для проверки сессии и роли.
func (s *Service) Store() *SQLStore { return s.st }

// lock сериализует изменения структуры папок одного владельца
// (проверка имени + создание/переименование/удаление).
func (s *Service) lock(owner string) func() {
	m, _ := s.locks.LoadOrStore(owner, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *Service) engineFor(owner string) (*Engine, error) {
	snap, err := s.st.Snapshot(owner)
	if err != nil {
		return nil, err
	}
	return NewEngine(snap), nil
}

// ---------- targets ----------

type target struct {
	owner User
	rel   string // после CleanRel
	abs   string
	info  os.FileInfo
	eng   *Engine // снимок правил владельца на момент запроса
}

func okKind(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink == 0 && (info.IsDir() || info.Mode().IsRegular())
}

// open находит существующий элемент. Если пользователь его не видит, ответ
// такой же, как для несуществующего: наличие скрытых файлов не раскрывается.
// Корень своей папки (и любой — у преподавателя) создаётся при первом обращении.
func (s *Service) open(actor Actor, owner, rel string) (*target, error) {
	if owner == "" {
		owner = actor.Login
	}
	u, ok, err := s.st.UserByLogin(owner)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, notFound()
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return nil, err
	}
	abs, err := Resolve(s.root, u.Login, clean)
	if err != nil {
		return nil, err
	}
	priv := canManage(actor, u.Login)

	info, err := os.Lstat(abs)
	if err != nil && clean == "" && priv && errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(abs, 0o755); err == nil {
			info, err = os.Lstat(abs)
		}
	}
	if err != nil || !okKind(info) || (clean == "" && !info.IsDir()) {
		return nil, notFound()
	}

	eng, err := s.engineFor(u.Login)
	if err != nil {
		return nil, err
	}
	if !priv {
		vis, err := eng.Visible(actor, u.Login, clean, info.IsDir())
		if err != nil {
			return nil, err
		}
		if !vis {
			return nil, notFound()
		}
	}
	return &target{owner: u, rel: clean, abs: abs, info: info, eng: eng}, nil
}

func (s *Service) require(t *target, actor Actor, a protocol.Action) error {
	ok, err := t.eng.Allowed(actor, t.owner.Login, t.rel, a)
	if err != nil {
		return err
	}
	if !ok {
		return forbidden()
	}
	return nil
}

// ---------- entries ----------

// entryFor описывает элемент для списка и заполняет Can (серые пункты меню).
func (s *Service) entryFor(eng *Engine, actor Actor, owner User, rel string, info os.FileInfo) (protocol.FileEntry, error) {
	isDir := info.IsDir()
	e := protocol.FileEntry{
		Name:      info.Name(),
		Path:      rel,
		IsDir:     isDir,
		ModTime:   info.ModTime().UnixMilli(),
		Owner:     owner.Login,
		OwnerName: owner.DisplayName(),
	}
	if rel == "" {
		e.Name = owner.DisplayName()
	}
	if !isDir {
		e.Size = info.Size()
	}

	can := map[protocol.Action]bool{}
	for _, a := range protocol.AllActions {
		if !isDir && !a.AppliesToFile() {
			continue
		}
		ok, err := eng.Allowed(actor, owner.Login, rel, a)
		if err != nil {
			return e, err
		}
		can[a] = ok
	}
	editable := isDir || EditableName(e.Name)
	if !editable {
		can[protocol.ActionEdit] = false
	}

	can[protocol.CapOpen] = !isDir && EditableName(e.Name) && can[protocol.ActionView]
	if rel == "" {
		can[protocol.CapRename] = false
		can[protocol.CapDuplicate] = false
	} else {
		switch {
		case canManage(actor, owner.Login):
			can[protocol.CapRename] = true
		default:
			can[protocol.CapRename] = editable && can[protocol.ActionEdit]
		}
		parentPaste, err := eng.Allowed(actor, owner.Login, Parent(rel), protocol.ActionPaste)
		if err != nil {
			return e, err
		}
		can[protocol.CapDuplicate] = can[protocol.ActionCopy] && parentPaste
	}
	e.Can = can
	return e, nil
}

// rootEntry — корень папки пользователя в списке «Сервер».
func (s *Service) rootEntry(eng *Engine, actor Actor, owner User) (protocol.FileEntry, error) {
	e := protocol.FileEntry{
		Name:      owner.DisplayName(),
		IsDir:     true,
		Owner:     owner.Login,
		OwnerName: owner.DisplayName(),
	}
	can := map[protocol.Action]bool{}
	for _, a := range protocol.AllActions {
		ok, err := eng.Allowed(actor, owner.Login, "", a)
		if err != nil {
			return e, err
		}
		can[a] = ok
	}
	can[protocol.CapOpen] = false
	can[protocol.CapRename] = false
	can[protocol.CapDuplicate] = false
	e.Can = can
	return e, nil
}

// ---------- disk helpers ----------

func dirNames(dir string) ([]string, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(des))
	for _, de := range des {
		out = append(out, de.Name())
	}
	return out, nil
}

// nameTaken ищет в папке имя без учёта регистра (Windows и macOS не различают
// регистр, и создание "A.txt" рядом с "a.txt" затёрло бы существующий файл).
func nameTaken(dir, name string) (existing string, taken bool, err error) {
	names, err := dirNames(dir)
	if err != nil {
		return "", false, err
	}
	key := Fold(name)
	for _, n := range names {
		if Fold(n) == key {
			return n, true, nil
		}
	}
	return "", false, nil
}

// treeSize — сумма размеров обычных файлов в поддереве (0, если папки нет).
func treeSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, e := d.Info(); e == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// ---------- quota ----------

// ensureUsage заводит учёт занятого места, если его ещё нет: считает обходом папки.
func (s *Service) ensureUsage(owner string) error {
	_, known, err := s.st.Usage(owner)
	if err != nil || known {
		return err
	}
	dir, err := OwnerDir(owner)
	if err != nil {
		return err
	}
	return s.st.SetUsage(owner, treeSize(filepath.Join(s.root, dir)))
}

func (s *Service) reserve(owner User, delta int64) error {
	if delta <= 0 {
		return nil
	}
	if err := s.ensureUsage(owner.Login); err != nil {
		return err
	}
	limit := LimitForRole(owner.Role, s.perUser())
	if err := s.st.ReserveUsage(owner.Login, delta, limit); err != nil {
		if errors.Is(err, ErrQuotaExceeded) {
			return newErr(protocol.FilesErrQuota, "not enough space")
		}
		return err
	}
	return nil
}

func (s *Service) release(owner string, delta int64) {
	if delta <= 0 {
		return
	}
	if err := s.st.ReleaseUsage(owner, delta); err != nil {
		slog.Error("files: release usage", "owner", owner, "err", err)
	}
}

// ---------- tree walking with permission checks ----------

type node struct {
	rel   string
	abs   string
	isDir bool
	size  int64
}

// permitted: элемент виден и для него разрешены все действия из need.
func (s *Service) permitted(actor Actor, eng *Engine, owner, rel string, isDir bool, need []protocol.Action) (bool, error) {
	vis, err := eng.Visible(actor, owner, rel, isDir)
	if err != nil || !vis {
		return false, err
	}
	for _, a := range need {
		ok, err := eng.Allowed(actor, owner, rel, a)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// gather обходит поддерево (сначала родители) и возвращает элементы, для
// которых permitted. Сам корень обхода включается всегда: его права
// проверяет вызывающий. Недоступные элементы пропускаются (skipped), а при
// strict любой такой элемент означает отказ для всей операции.
// Символические ссылки, служебные и неадресуемые имена игнорируются.
func (s *Service) gather(actor Actor, eng *Engine, owner string, priv bool, rel, abs string,
	info os.FileInfo, need []protocol.Action, strict bool) ([]node, int, error) {

	var nodes []node
	skipped := 0
	var walk func(rel, abs string, info os.FileInfo) error
	walk = func(rel, abs string, info os.FileInfo) error {
		n := node{rel: rel, abs: abs, isDir: info.IsDir()}
		if !n.isDir {
			n.size = info.Size()
		}
		nodes = append(nodes, n)
		if !n.isDir {
			return nil
		}
		des, err := os.ReadDir(abs)
		if err != nil {
			return err
		}
		for _, de := range des {
			name := de.Name()
			if strings.HasPrefix(name, TempPrefix) {
				continue
			}
			if c, err := CleanName(name); err != nil || c != name {
				continue
			}
			cinfo, err := de.Info()
			if err != nil || !okKind(cinfo) {
				continue
			}
			crel := Join(rel, name)
			if !priv {
				ok, err := s.permitted(actor, eng, owner, crel, cinfo.IsDir(), need)
				if err != nil {
					return err
				}
				if !ok {
					if strict {
						return forbidden()
					}
					skipped++
					continue
				}
			}
			if err := walk(crel, filepath.Join(abs, name), cinfo); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(rel, abs, info); err != nil {
		return nil, 0, err
	}
	return nodes, skipped, nil
}

// ---------- dispatch ----------

// Do выполняет запрос от имени actor. Актёр определяется по сессии (роль из
// БД), а не из запроса.
func (s *Service) Do(actor Actor, req protocol.FilesRequest) protocol.FilesResponse {
	resp, err := s.do(actor, req)
	if err != nil {
		return respErr(err)
	}
	resp.OK = true
	return resp
}

func (s *Service) do(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	switch req.Op {
	case protocol.FilesOpListOwners:
		return s.listOwners(actor)
	case protocol.FilesOpList:
		return s.doList(actor, req)
	case protocol.FilesOpStat:
		return s.doStat(actor, req)
	case protocol.FilesOpMkdir:
		return s.createChild(actor, req, true)
	case protocol.FilesOpCreate:
		return s.createChild(actor, req, false)
	case protocol.FilesOpRename:
		return s.doRename(actor, req)
	case protocol.FilesOpDelete:
		return s.doDelete(actor, req)
	case protocol.FilesOpSearch:
		return s.doSearch(actor, req)
	case protocol.FilesOpSend:
		return s.doSend(actor, req)
	case protocol.FilesOpBlockedPut:
		return s.blockedPut(actor, req)
	case protocol.FilesOpPaste:
		return s.doPaste(actor, req)
	case protocol.FilesOpDuplicate:
		return s.doDuplicate(actor, req)
	case protocol.FilesOpRulesGet:
		return s.rulesGet(actor, req)
	case protocol.FilesOpRuleSet:
		return s.ruleSet(actor, req)
	case protocol.FilesOpRuleClear:
		return s.ruleClear(actor, req)
	case protocol.FilesOpQuota:
		return s.doQuota(actor, req)
	case protocol.FilesOpFavAdd:
		return s.favAdd(actor, req)
	case protocol.FilesOpFavRemove:
		return s.favRemove(actor, req)
	case protocol.FilesOpFavList:
		return s.favList(actor)
	case protocol.FilesOpUIGet:
		return s.uiGet(actor)
	case protocol.FilesOpUIPut:
		return s.uiPut(actor, req)
	case protocol.FilesOpContactsPut:
		return s.contactsPut(actor, req)
	}
	return protocol.FilesResponse{}, newErr(protocol.FilesErrInvalidRequest, "unknown or unsupported operation")
}

// PurgeUser удаляет папку пользователя и все его данные в таблицах файлов.
// Вызывать после удаления пользователя из БД.
func (s *Service) PurgeUser(login string) error {
	dir, err := OwnerDir(login)
	if err != nil {
		return err
	}
	unlock := s.lock(login)
	defer unlock()
	if err := os.RemoveAll(filepath.Join(s.root, dir)); err != nil {
		return err
	}
	return s.st.PurgeUser(login)
}
