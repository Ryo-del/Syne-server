package files

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	protocol "github.com/Ryo-del/Syne-protocol"
)

const (
	defaultSearchLimit = 100
	maxSearchLimit     = 200
	maxSearchQueryLen  = 100
	searchTimeBudget   = 8 * time.Second
	maxSearchVisited   = 200000
	maxSearchFileBytes = int64(1) << 20  // .txt крупнее по содержимому не просматривается
	maxSearchScanBytes = int64(64) << 20 // всего на один запрос
	maxSnippetRunes    = 160
)

type searchState struct {
	needle     string
	content    bool
	limit      int
	deadline   time.Time
	visited    int
	scanBudget int64
	results    []protocol.FileEntry
	truncated  bool
	halted     bool
}

func (st *searchState) stop() bool {
	if st.halted {
		return true
	}
	if st.visited >= maxSearchVisited || time.Now().After(st.deadline) {
		st.truncated, st.halted = true, true
	}
	return st.halted
}

// doSearch ищет по имени (подстрока без учёта регистра) и, если Content,
// по содержимому .txt. Owner пусто — везде, где пользователь что-то видит;
// иначе только в папке этого владельца.
func (s *Service) doSearch(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" || utf8.RuneCountInString(query) > maxSearchQueryLen {
		return protocol.FilesResponse{}, invalid("query must be 1..100 characters")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	st := &searchState{
		needle:     Fold(query),
		content:    req.Content,
		limit:      limit,
		deadline:   time.Now().Add(searchTimeBudget),
		scanBudget: maxSearchScanBytes,
	}

	owners, err := s.searchOwners(actor, req.Owner)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	for _, o := range owners {
		if st.stop() {
			break
		}
		if err := s.searchOwner(st, actor, o); err != nil {
			return protocol.FilesResponse{}, err
		}
	}
	return protocol.FilesResponse{Entries: st.results, Truncated: st.truncated}, nil
}

// searchOwners: сначала сам пользователь, затем остальные по имени.
// Преподаватель просматривает всех, остальные только тех, у кого для них
// что-то открыто.
func (s *Service) searchOwners(actor Actor, only string) ([]User, error) {
	users, err := s.st.AllUsers()
	if err != nil {
		return nil, err
	}
	byLogin := make(map[string]User, len(users))
	for _, u := range users {
		byLogin[u.Login] = u
	}

	var out []User
	if me, ok := byLogin[actor.Login]; ok && (only == "" || only == actor.Login) {
		out = append(out, me)
	}
	if only == actor.Login {
		return out, nil
	}

	var candidates map[string]bool
	if !actor.IsTeacher() {
		logins, err := s.st.OwnersWithVisibleRules()
		if err != nil {
			return nil, err
		}
		candidates = make(map[string]bool, len(logins))
		for _, l := range logins {
			candidates[l] = true
		}
	}
	var others []User
	for _, u := range users {
		if u.Login == actor.Login || (only != "" && u.Login != only) {
			continue
		}
		if candidates != nil && !candidates[u.Login] {
			continue
		}
		others = append(others, u)
	}
	sort.Slice(others, func(i, j int) bool {
		a, b := strings.ToLower(others[i].DisplayName()), strings.ToLower(others[j].DisplayName())
		if a != b {
			return a < b
		}
		return others[i].Login < others[j].Login
	})
	return append(out, others...), nil
}

// searchOwner обходит папку владельца в глубину. Для чужих папок поддерево,
// которое пользователь не видит, пропускается целиком.
func (s *Service) searchOwner(st *searchState, actor Actor, owner User) error {
	priv := canManage(actor, owner.Login)
	eng, err := s.engineFor(owner.Login)
	if err != nil {
		return err
	}
	if !priv {
		vis, err := eng.Visible(actor, owner.Login, "", true)
		if err != nil {
			return err
		}
		if !vis {
			return nil
		}
	}
	base, err := Resolve(s.root, owner.Login, "")
	if err != nil {
		return err
	}
	if _, err := os.Lstat(base); err != nil {
		return nil // папки ещё нет: искать нечего
	}

	var walkErr error
	var walk func(abs, rel string)
	walk = func(abs, rel string) {
		des, err := os.ReadDir(abs)
		if err != nil {
			return
		}
		for _, de := range des {
			if st.stop() || walkErr != nil {
				return
			}
			name := de.Name()
			if strings.HasPrefix(name, TempPrefix) {
				continue
			}
			if c, err := CleanName(name); err != nil || c != name {
				continue
			}
			info, err := de.Info()
			if err != nil || !okKind(info) {
				continue
			}
			st.visited++
			crel := Join(rel, name)
			if !priv {
				vis, err := eng.Visible(actor, owner.Login, crel, info.IsDir())
				if err != nil {
					walkErr = err
					return
				}
				if !vis {
					continue
				}
			}

			hit := strings.Contains(Fold(name), st.needle)
			snippet := ""
			if st.content && !info.IsDir() && EditableName(name) {
				snippet = scanSnippet(st, filepath.Join(abs, name), info)
			}
			if hit || snippet != "" {
				if len(st.results) >= st.limit {
					st.truncated, st.halted = true, true
					return
				}
				e, err := s.entryFor(eng, actor, owner, crel, info)
				if err != nil {
					walkErr = err
					return
				}
				e.Snippet = snippet
				st.results = append(st.results, e)
			}
			if info.IsDir() {
				walk(filepath.Join(abs, name), crel)
			}
		}
	}
	walk(base, "")
	return walkErr
}

// scanSnippet возвращает первую строку файла с совпадением (пусто — нет).
// Читаются только небольшие текстовые файлы; бинарные пропускаются.
func scanSnippet(st *searchState, abs string, info os.FileInfo) string {
	if info.Size() > maxSearchFileBytes || st.scanBudget <= 0 {
		return ""
	}
	f, _, err := openRegular(abs)
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSearchFileBytes+1))
	if err != nil || int64(len(data)) > maxSearchFileBytes {
		return ""
	}
	st.scanBudget -= int64(len(data))
	if bytes.IndexByte(data, 0) >= 0 {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(Fold(line), st.needle) {
			return trimSnippet(line)
		}
	}
	return ""
}

func trimSnippet(line string) string {
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) <= maxSnippetRunes {
		return line
	}
	return string([]rune(line)[:maxSnippetRunes]) + "…"
}
