package files

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	protocol "github.com/Ryo-del/Syne-protocol"
)

var (
	ErrForbidden   = errors.New("forbidden")
	ErrInvalidRule = errors.New("invalid rule")
)

const maxRuleUsers = 500

// IsTeacherRole — преподаватель (роль users.role).
func IsTeacherRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), protocol.RoleTeacher)
}

// Actor — кто выполняет действие. Роль берётся из БД по сессии, не от клиента.
type Actor struct {
	Login string
	Role  string
}

func (a Actor) IsTeacher() bool { return IsTeacherRole(a.Role) }

// Rule — правило на конкретном пути. Users заполнен только для ModeSelected.
type Rule struct {
	Mode  protocol.Mode
	Users []string
}

// PathRule — правило вместе с путём и действием (для запросов по поддереву).
type PathRule struct {
	Path   string
	Action protocol.Action
	Rule   Rule
}

// Effective — правило, действующее для пути (своё или унаследованное).
type Effective struct {
	Rule  Rule
	From  string // путь, на котором правило задано
	Found bool   // false: правил нет, действует «запретить всем»
}

// Store — хранилище правил. Все пути приходят уже в форме Fold(CleanRel(..)).
type Store interface {
	GetRule(owner, path string, action protocol.Action) (Rule, bool, error)
	PutRule(owner, path string, action protocol.Action, r Rule) error
	DeleteRule(owner, path string, action protocol.Action) error
	// RulesUnder — правила строго внутри поддерева dir (сам dir не включается)
	// для перечисленных действий.
	RulesUnder(owner, dir string, actions ...protocol.Action) ([]PathRule, error)
	// IsContact — contact находится в списке контактов владельца owner.
	IsContact(owner, contact string) (bool, error)
}

type Engine struct{ st Store }

func NewEngine(st Store) *Engine { return &Engine{st: st} }

// Effective ищет правило на самом пути, затем на каждом родителе до корня.
// Первое найденное побеждает. Правил нет — Found=false.
func (e *Engine) Effective(owner, rel string, action protocol.Action) (Effective, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return Effective{}, err
	}
	for _, p := range Ancestors(Fold(clean)) {
		r, ok, err := e.st.GetRule(owner, p, action)
		if err != nil {
			return Effective{}, err
		}
		if ok {
			return Effective{Rule: r, From: p, Found: true}, nil
		}
	}
	return Effective{}, nil
}

// Allowed — главная проверка. Порядок:
//  1. неверный путь или действие — ошибка (доступ закрыт);
//  2. преподаватель — всегда да;
//  3. владелец — всегда да (его собственные правила ограничивают других);
//  4. иначе действующее правило; правил нет — нет.
func (e *Engine) Allowed(actor Actor, owner, rel string, action protocol.Action) (bool, error) {
	if !action.Valid() {
		return false, fmt.Errorf("%w: unknown action %q", ErrInvalidRule, action)
	}
	if _, err := CleanRel(rel); err != nil {
		return false, err
	}
	if actor.IsTeacher() || (actor.Login != "" && actor.Login == owner) {
		return true, nil
	}
	if actor.Login == "" {
		return false, nil
	}
	eff, err := e.Effective(owner, rel, action)
	if err != nil || !eff.Found {
		return false, err
	}
	return e.matches(actor, owner, eff.Rule)
}

func (e *Engine) matches(actor Actor, owner string, r Rule) (bool, error) {
	switch r.Mode {
	case protocol.ModeAll:
		return true, nil
	case protocol.ModeContacts:
		return e.st.IsContact(owner, actor.Login)
	case protocol.ModeSelected:
		for _, u := range r.Users {
			if u == actor.Login {
				return true, nil
			}
		}
		return false, nil
	default: // ModeNone и любое неизвестное значение — закрыто
		return false, nil
	}
}

// Visible — виден ли элемент пользователю в дереве.
//
// Файл виден, если разрешён view. Папка видна, если разрешён view или paste,
// либо внутри неё есть хотя бы один элемент с подходящим правилом. Так
// остаётся только цепочка папок до разрешённых файлов, а остальное скрыто.
func (e *Engine) Visible(actor Actor, owner, rel string, isDir bool) (bool, error) {
	ok, err := e.Allowed(actor, owner, rel, protocol.ActionView)
	if err != nil || ok {
		return ok, err
	}
	if !isDir {
		return false, nil
	}
	ok, err = e.Allowed(actor, owner, rel, protocol.ActionPaste)
	if err != nil || ok {
		return ok, err
	}
	clean, _ := CleanRel(rel) // уже проверен в Allowed
	rules, err := e.st.RulesUnder(owner, Fold(clean), protocol.ActionView, protocol.ActionPaste)
	if err != nil {
		return false, err
	}
	for _, pr := range rules {
		m, err := e.matches(actor, owner, pr.Rule)
		if err != nil {
			return false, err
		}
		if m {
			return true, nil
		}
	}
	return false, nil
}

func canManage(actor Actor, owner string) bool {
	return actor.IsTeacher() || (actor.Login != "" && actor.Login == owner)
}

// checkRuleTarget — общие ограничения на то, где какое право можно задать.
func checkRuleTarget(clean string, isDir bool, action protocol.Action) error {
	if !action.Valid() {
		return fmt.Errorf("%w: unknown action %q", ErrInvalidRule, action)
	}
	if !isDir && action == protocol.ActionPaste {
		return fmt.Errorf("%w: paste is folder-only", ErrInvalidRule)
	}
	if !isDir && action == protocol.ActionEdit && !EditableName(Base(clean)) {
		return fmt.Errorf("%w: only .txt files are editable", ErrInvalidRule)
	}
	return nil
}

// SetRule задаёт правило. Менять права может только владелец или преподаватель.
func (e *Engine) SetRule(actor Actor, owner, rel string, isDir bool,
	action protocol.Action, mode protocol.Mode, users []string) error {
	if !canManage(actor, owner) {
		return ErrForbidden
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return err
	}
	if err := checkRuleTarget(clean, isDir, action); err != nil {
		return err
	}
	if !mode.Valid() {
		return fmt.Errorf("%w: unknown mode %q", ErrInvalidRule, mode)
	}
	var list []string
	if mode == protocol.ModeSelected {
		list = normalizeUsers(users)
		if len(list) == 0 {
			return fmt.Errorf("%w: select at least one user", ErrInvalidRule)
		}
		if len(list) > maxRuleUsers {
			return fmt.Errorf("%w: too many users", ErrInvalidRule)
		}
	}
	return e.st.PutRule(owner, Fold(clean), action, Rule{Mode: mode, Users: list})
}

// ClearRule убирает правило с самого элемента: дальше он наследует от папки.
func (e *Engine) ClearRule(actor Actor, owner, rel string, action protocol.Action) error {
	if !canManage(actor, owner) {
		return ErrForbidden
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return err
	}
	if !action.Valid() {
		return fmt.Errorf("%w: unknown action %q", ErrInvalidRule, action)
	}
	return e.st.DeleteRule(owner, Fold(clean), action)
}

// Describe — данные для диалога «Настроить права». Для действий без своего
// правила возвращается унаследованное (или «запретить всем» по умолчанию)
// с Inherited=true.
func (e *Engine) Describe(actor Actor, owner, rel string, isDir bool) ([]protocol.ACLRule, error) {
	if !canManage(actor, owner) {
		return nil, ErrForbidden
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return nil, err
	}
	key := Fold(clean)
	var out []protocol.ACLRule
	for _, a := range protocol.AllActions {
		if checkRuleTarget(clean, isDir, a) != nil {
			continue // например, paste для файла или edit для не-txt
		}
		eff, err := e.Effective(owner, clean, a)
		if err != nil {
			return nil, err
		}
		item := protocol.ACLRule{Action: a, Mode: protocol.ModeNone, Inherited: true}
		if eff.Found {
			item.Mode = eff.Rule.Mode
			item.Users = eff.Rule.Users
			item.Inherited = eff.From != key
		}
		out = append(out, item)
	}
	return out, nil
}

func normalizeUsers(users []string) []string {
	seen := make(map[string]struct{}, len(users))
	out := make([]string, 0, len(users))
	for _, u := range users {
		if u == "" {
			continue
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}
