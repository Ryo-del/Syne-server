package files

import (
	"errors"
	"strings"
	"testing"

	protocol "github.com/Ryo-del/Syne-protocol"
)

// ---------- in-memory Store for tests ----------

type memRule struct {
	owner string
	PathRule
}

type memStore struct {
	rules    map[string]memRule
	contacts map[string]bool
}

func newMemStore() *memStore {
	return &memStore{rules: map[string]memRule{}, contacts: map[string]bool{}}
}

func memKey(owner, path string, a protocol.Action) string {
	return owner + "\x00" + path + "\x00" + string(a)
}

func (m *memStore) GetRule(owner, path string, a protocol.Action) (Rule, bool, error) {
	r, ok := m.rules[memKey(owner, path, a)]
	return r.Rule, ok, nil
}

func (m *memStore) PutRule(owner, path string, a protocol.Action, r Rule) error {
	m.rules[memKey(owner, path, a)] = memRule{owner: owner, PathRule: PathRule{Path: path, Action: a, Rule: r}}
	return nil
}

func (m *memStore) DeleteRule(owner, path string, a protocol.Action) error {
	delete(m.rules, memKey(owner, path, a))
	return nil
}

func (m *memStore) RulesUnder(owner, dir string, actions ...protocol.Action) ([]PathRule, error) {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	var out []PathRule
	for _, r := range m.rules {
		if r.owner != owner || r.Path == "" || !strings.HasPrefix(r.Path, prefix) {
			continue
		}
		for _, a := range actions {
			if r.Action == a {
				out = append(out, r.PathRule)
				break
			}
		}
	}
	return out, nil
}

func (m *memStore) IsContact(owner, contact string) (bool, error) {
	return m.contacts[owner+"\x00"+contact], nil
}

func TestEngineWithMemStore(t *testing.T) {
	st := newMemStore()
	runEngineScenario(t, st, func(owner, contact string) {
		st.contacts[owner+"\x00"+contact] = true
	})
}

// runEngineScenario прогоняет одни и те же проверки на любом Store
// (память и SQLite).
func runEngineScenario(t *testing.T, st Store, addContact func(owner, contact string)) {
	t.Helper()
	e := NewEngine(st)
	teacher := Actor{Login: "t1", Role: "teacher"}
	alice := Actor{Login: "alice", Role: "student"}
	bob := Actor{Login: "bob", Role: "student"}
	carol := Actor{Login: "carol", Role: "student"}

	allowed := func(a Actor, rel string, act protocol.Action, want bool) {
		t.Helper()
		got, err := e.Allowed(a, "alice", rel, act)
		if err != nil {
			t.Fatalf("Allowed(%s, %q, %s): %v", a.Login, rel, act, err)
		}
		if got != want {
			t.Errorf("Allowed(%s, %q, %s) = %v, want %v", a.Login, rel, act, got, want)
		}
	}
	visible := func(a Actor, rel string, isDir, want bool) {
		t.Helper()
		got, err := e.Visible(a, "alice", rel, isDir)
		if err != nil {
			t.Fatalf("Visible(%s, %q): %v", a.Login, rel, err)
		}
		if got != want {
			t.Errorf("Visible(%s, %q, dir=%v) = %v, want %v", a.Login, rel, isDir, got, want)
		}
	}
	set := func(by Actor, rel string, isDir bool, act protocol.Action, mode protocol.Mode, users ...string) {
		t.Helper()
		if err := e.SetRule(by, "alice", rel, isDir, act, mode, users); err != nil {
			t.Fatalf("SetRule(%q, %s, %s): %v", rel, act, mode, err)
		}
	}

	// 1. по умолчанию чужим закрыто; владелец и преподаватель всегда могут
	allowed(bob, "f.txt", protocol.ActionView, false)
	allowed(alice, "f.txt", protocol.ActionView, true)
	allowed(teacher, "f.txt", protocol.ActionDelete, true)

	// 2. правило на папке наследуется вложенными элементами
	set(alice, "docs", true, protocol.ActionView, protocol.ModeAll)
	allowed(bob, "docs", protocol.ActionView, true)
	allowed(bob, "docs/a.txt", protocol.ActionView, true)
	allowed(bob, "other.txt", protocol.ActionView, false)
	allowed(bob, "docs/a.txt", protocol.ActionDownload, false)

	// 3. правило на файле перекрывает папку; владелец и преподаватель не ограничены
	set(alice, "docs/secret.txt", false, protocol.ActionView, protocol.ModeNone)
	allowed(bob, "docs/secret.txt", protocol.ActionView, false)
	allowed(bob, "docs/a.txt", protocol.ActionView, true)
	allowed(alice, "docs/secret.txt", protocol.ActionView, true)
	allowed(teacher, "docs/secret.txt", protocol.ActionView, true)

	// 3b. ClearRule возвращает наследование
	if err := e.ClearRule(alice, "alice", "docs/secret.txt", protocol.ActionView); err != nil {
		t.Fatal(err)
	}
	allowed(bob, "docs/secret.txt", protocol.ActionView, true)

	// 4. выделенные пользователи
	set(alice, "docs", true, protocol.ActionDownload, protocol.ModeSelected, "bob", "bob")
	allowed(bob, "docs/a.txt", protocol.ActionDownload, true)
	allowed(carol, "docs/a.txt", protocol.ActionDownload, false)

	// 5. контакты: список контактов ВЛАДЕЛЬЦА
	set(alice, "docs", true, protocol.ActionSend, protocol.ModeContacts)
	addContact("alice", "bob")
	addContact("carol", "alice") // alice в контактах у carol, но не наоборот
	allowed(bob, "docs/a.txt", protocol.ActionSend, true)
	allowed(carol, "docs/a.txt", protocol.ActionSend, false)

	// 6. регистр не важен (Windows/macOS)
	set(alice, "Docs/Case.txt", false, protocol.ActionView, protocol.ModeAll)
	allowed(bob, "docs/case.TXT", protocol.ActionView, true)

	// 7. видимость: остаётся цепочка папок до разрешённого файла
	set(alice, "a/b/file.txt", false, protocol.ActionView, protocol.ModeAll)
	visible(bob, "", true, true)
	visible(bob, "a", true, true)
	visible(bob, "a/b", true, true)
	visible(bob, "a/b/file.txt", false, true)
	visible(bob, "a/other", true, false)
	visible(bob, "a/b/x.txt", false, false)
	visible(bob, "z", true, false)
	visible(bob, "docs", true, true)

	// 7b. папка с правом вставки видна, но её файлы — нет
	set(alice, "up", true, protocol.ActionPaste, protocol.ModeAll)
	visible(bob, "up", true, true)
	visible(bob, "up/x.txt", false, false)
	allowed(bob, "up/new.txt", protocol.ActionPaste, true)

	// 8. проверки при выдаче прав
	if err := e.SetRule(bob, "alice", "docs", true, protocol.ActionView, protocol.ModeAll, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-owner SetRule: %v", err)
	}
	if err := e.SetRule(teacher, "alice", "docs", true, protocol.ActionView, protocol.ModeAll, nil); err != nil {
		t.Errorf("teacher SetRule: %v", err)
	}
	invalid := []struct {
		name  string
		rel   string
		isDir bool
		act   protocol.Action
		mode  protocol.Mode
	}{
		{"paste on file", "f.txt", false, protocol.ActionPaste, protocol.ModeAll},
		{"edit on non-txt", "pic.png", false, protocol.ActionEdit, protocol.ModeAll},
		{"selected without users", "docs", true, protocol.ActionView, protocol.ModeSelected},
		{"unknown action", "docs", true, protocol.Action("fly"), protocol.ModeAll},
		{"unknown mode", "docs", true, protocol.ActionView, protocol.Mode("maybe")},
	}
	for _, c := range invalid {
		if err := e.SetRule(alice, "alice", c.rel, c.isDir, c.act, c.mode, nil); !errors.Is(err, ErrInvalidRule) {
			t.Errorf("%s: got %v, want ErrInvalidRule", c.name, err)
		}
	}

	// 9. неверный путь — ошибка, доступ закрыт
	if _, err := e.Allowed(bob, "alice", "../x", protocol.ActionView); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("invalid path: %v", err)
	}

	// 10. Describe для диалога прав
	rules, err := e.Describe(alice, "alice", "docs/a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	var sawView, sawPaste, sawEdit bool
	for _, r := range rules {
		switch r.Action {
		case protocol.ActionView:
			sawView = true
			if r.Mode != protocol.ModeAll || !r.Inherited {
				t.Errorf("view rule = %+v, want inherited all", r)
			}
		case protocol.ActionPaste:
			sawPaste = true
		case protocol.ActionEdit:
			sawEdit = true
		}
	}
	if !sawView || sawPaste || !sawEdit {
		t.Errorf("Describe(file): view=%v paste=%v edit=%v", sawView, sawPaste, sawEdit)
	}
	if _, err := e.Describe(bob, "alice", "docs", true); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-owner Describe: %v", err)
	}
}

func TestQuota(t *testing.T) {
	got, err := QuotaPerUser(500, 50)
	if err != nil || got != 10*gib {
		t.Errorf("QuotaPerUser(500, 50) = %d, %v; want %d", got, err, 10*gib)
	}
	if _, err := QuotaPerUser(0, 50); err == nil {
		t.Error("zero memory must be rejected")
	}
	if _, err := QuotaPerUser(500, 0); err == nil {
		t.Error("zero users must be rejected")
	}
	if LimitForRole("teacher", 5) != Unlimited || LimitForRole("student", 5) != 5 {
		t.Error("LimitForRole")
	}
}
