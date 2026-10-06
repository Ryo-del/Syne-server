package files

import (
	"os"
	"path/filepath"
	"strings"

	protocol "github.com/Ryo-del/Syne-protocol"
)

// ---------- rules ----------

func (s *Service) rulesGet(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	rules, err := s.eng.Describe(actor, t.owner.Login, t.rel, t.info.IsDir())
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{Rules: rules}, nil
}

func (s *Service) ruleSet(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if req.Mode == protocol.ModeSelected {
		users, err := s.st.AllUsers()
		if err != nil {
			return protocol.FilesResponse{}, err
		}
		known := make(map[string]bool, len(users))
		for _, u := range users {
			known[u.Login] = true
		}
		for _, l := range req.Users {
			if !known[l] {
				return protocol.FilesResponse{}, invalid("unknown user in the list")
			}
		}
	}
	if err := s.eng.SetRule(actor, t.owner.Login, t.rel, t.info.IsDir(), req.Action, req.Mode, req.Users); err != nil {
		return protocol.FilesResponse{}, err
	}
	rules, err := s.eng.Describe(actor, t.owner.Login, t.rel, t.info.IsDir())
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{Rules: rules}, nil
}

func (s *Service) ruleClear(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if err := s.eng.ClearRule(actor, t.owner.Login, t.rel, req.Action); err != nil {
		return protocol.FilesResponse{}, err
	}
	rules, err := s.eng.Describe(actor, t.owner.Login, t.rel, t.info.IsDir())
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{Rules: rules}, nil
}

// ---------- quota ----------

// doQuota — индикатор места для своей папки (преподаватель может спросить чужую).
func (s *Service) doQuota(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	owner := req.Owner
	if owner == "" {
		owner = actor.Login
	}
	if owner != actor.Login && !actor.IsTeacher() {
		return protocol.FilesResponse{}, forbidden()
	}
	u, ok, err := s.st.UserByLogin(owner)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if !ok {
		return protocol.FilesResponse{}, notFound()
	}
	if err := s.ensureUsage(owner); err != nil {
		return protocol.FilesResponse{}, err
	}
	used, _, err := s.st.Usage(owner)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	limit := LimitForRole(u.Role, s.perUser())
	q := &protocol.QuotaInfo{UsedBytes: used}
	if limit < 0 {
		q.Unlimited = true
	} else {
		q.LimitBytes = limit
	}
	return protocol.FilesResponse{Quota: q}, nil
}

// ---------- favorites ----------

// favAdd — ссылка на элемент, который пользователь видит.
func (s *Service) favAdd(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if err := s.st.AddFavorite(actor.Login, t.owner.Login, Fold(t.rel)); err != nil {
		return protocol.FilesResponse{}, err
	}
	e, err := s.entryFor(t.eng, actor, t.owner, t.rel, t.info)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{Entry: &e}, nil
}

// favRemove не требует, чтобы элемент ещё существовал.
func (s *Service) favRemove(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	owner := req.Owner
	if owner == "" {
		owner = actor.Login
	}
	clean, err := CleanRel(req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if err := s.st.RemoveFavorite(actor.Login, owner, Fold(clean)); err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{}, nil
}

// resolveFolded по ключу Fold(путь) находит реальный элемент на диске
// (с настоящим регистром имён). Ссылки на нём не раскрываются.
func (s *Service) resolveFolded(owner, key string) (rel string, info os.FileInfo, err error) {
	dir, err := OwnerDir(owner)
	if err != nil {
		return "", nil, err
	}
	abs := filepath.Join(s.root, dir)
	if key != "" {
		for _, seg := range strings.Split(key, "/") {
			names, err := dirNames(abs)
			if err != nil {
				return "", nil, os.ErrNotExist
			}
			found := ""
			for _, n := range names {
				if Fold(n) == seg {
					found = n
					break
				}
			}
			if found == "" {
				return "", nil, os.ErrNotExist
			}
			abs = filepath.Join(abs, found)
			rel = Join(rel, found)
		}
	}
	safe, err := Resolve(s.root, owner, rel)
	if err != nil {
		return "", nil, os.ErrNotExist
	}
	info, err = os.Lstat(safe)
	if err != nil || !okKind(info) {
		return "", nil, os.ErrNotExist
	}
	return rel, info, nil
}

// favList возвращает ссылки, которые пользователь всё ещё видит. Недоступные
// или исчезнувшие ссылки удаляются: если доступ закрыли, ссылка пропадает.
func (s *Service) favList(actor Actor) (protocol.FilesResponse, error) {
	favs, err := s.st.ListFavorites(actor.Login)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	users := map[string]User{}
	engines := map[string]*Engine{}
	drop := func(f Favorite) {
		_ = s.st.RemoveFavorite(actor.Login, f.Owner, f.PathKey)
	}

	var out []protocol.FileEntry
	for _, f := range favs {
		u, ok := users[f.Owner]
		if !ok {
			found := false
			u, found, err = s.st.UserByLogin(f.Owner)
			if err != nil {
				return protocol.FilesResponse{}, err
			}
			if !found {
				drop(f)
				continue
			}
			users[f.Owner] = u
		}
		rel, info, err := s.resolveFolded(f.Owner, f.PathKey)
		if err != nil {
			drop(f)
			continue
		}
		eng, ok := engines[f.Owner]
		if !ok {
			eng, err = s.engineFor(f.Owner)
			if err != nil {
				return protocol.FilesResponse{}, err
			}
			engines[f.Owner] = eng
		}
		if !canManage(actor, f.Owner) {
			vis, err := eng.Visible(actor, f.Owner, rel, info.IsDir())
			if err != nil {
				return protocol.FilesResponse{}, err
			}
			if !vis {
				drop(f)
				continue
			}
		}
		e, err := s.entryFor(eng, actor, u, rel, info)
		if err != nil {
			return protocol.FilesResponse{}, err
		}
		out = append(out, e)
	}
	return protocol.FilesResponse{Entries: out}, nil
}

// ---------- UI state / contacts ----------

func (s *Service) uiGet(actor Actor) (protocol.FilesResponse, error) {
	data, _, err := s.st.GetUIState(actor.Login)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{Data: data}, nil
}

func (s *Service) uiPut(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	if err := s.st.PutUIState(actor.Login, req.Data); err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{}, nil
}

// contactsPut — клиент присылает свой список контактов целиком (для режима
// «разрешить только контактам»).
func (s *Service) contactsPut(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	if len(req.Contacts) > maxContacts {
		return protocol.FilesResponse{}, invalid("too many contacts")
	}
	if err := s.st.ReplaceContacts(actor.Login, req.Contacts); err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{}, nil
}

// blockedPut — клиент присылает свой список блокировок целиком.
func (s *Service) blockedPut(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	if len(req.Blocked) > maxContacts {
		return protocol.FilesResponse{}, invalid("too many entries")
	}
	if err := s.st.ReplaceBlocked(actor.Login, req.Blocked); err != nil {
		return protocol.FilesResponse{}, err
	}
	return protocol.FilesResponse{}, nil
}
