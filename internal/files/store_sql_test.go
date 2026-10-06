package files

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	protocol "github.com/Ryo-del/Syne-protocol"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := InitSchema(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestInitSchemaIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := InitSchema(db); err != nil {
		t.Fatalf("second InitSchema: %v", err)
	}
}

func TestSQLStoreEngineScenario(t *testing.T) {
	st := NewSQLStore(openTestDB(t))
	runEngineScenario(t, st, func(owner, contact string) {
		if err := st.AddContact(owner, contact); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSQLStoreSelectedRoundtrip(t *testing.T) {
	st := NewSQLStore(openTestDB(t))
	want := Rule{Mode: protocol.ModeSelected, Users: []string{"bob", "carol"}}
	if err := st.PutRule("alice", "docs", protocol.ActionView, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetRule("alice", "docs", protocol.ActionView)
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v ok=%v err=%v, want %+v", got, ok, err, want)
	}

	// смена режима очищает список пользователей
	if err := st.PutRule("alice", "docs", protocol.ActionView, Rule{Mode: protocol.ModeAll}); err != nil {
		t.Fatal(err)
	}
	got, _, _ = st.GetRule("alice", "docs", protocol.ActionView)
	if got.Mode != protocol.ModeAll || len(got.Users) != 0 {
		t.Errorf("after mode change: %+v", got)
	}
	if err := st.PutRule("alice", "docs", protocol.ActionView, Rule{Mode: protocol.ModeSelected, Users: []string{"dave"}}); err != nil {
		t.Fatal(err)
	}
	got, _, _ = st.GetRule("alice", "docs", protocol.ActionView)
	if !reflect.DeepEqual(got.Users, []string{"dave"}) {
		t.Errorf("users after re-select: %v", got.Users)
	}
}

func rulePaths(rules []PathRule) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.Path)
	}
	sort.Strings(out)
	return out
}

func TestSQLStoreRulesUnderPrefix(t *testing.T) {
	st := NewSQLStore(openTestDB(t))
	all := Rule{Mode: protocol.ModeAll}
	for _, p := range []string{"", "a", "a/x", "a/y/z", "ab/q", "a_b/w", "a%b/w"} {
		if err := st.PutRule("alice", p, protocol.ActionView, all); err != nil {
			t.Fatal(err)
		}
	}
	// чужой владелец и другое действие не должны попадать в выборку
	_ = st.PutRule("bob", "a/x", protocol.ActionView, all)
	_ = st.PutRule("alice", "a/dl", protocol.ActionDownload, all)

	cases := []struct {
		dir  string
		want []string
	}{
		{"a", []string{"a/x", "a/y/z"}},
		{"a/y", []string{"a/y/z"}},
		{"a_b", []string{"a_b/w"}},
		{"a%b", []string{"a%b/w"}},
		{"", []string{"a", "a%b/w", "a/x", "a/y/z", "a_b/w", "ab/q"}},
		{"zzz", []string{}},
	}
	for _, c := range cases {
		rules, err := st.RulesUnder("alice", c.dir, protocol.ActionView)
		if err != nil {
			t.Fatal(err)
		}
		got := rulePaths(rules)
		sort.Strings(c.want)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("RulesUnder(%q) = %v, want %v", c.dir, got, c.want)
		}
	}
}

func TestSQLStoreRenameAndDeletePrefix(t *testing.T) {
	st := NewSQLStore(openTestDB(t))
	all := Rule{Mode: protocol.ModeAll}
	_ = st.PutRule("alice", "old", protocol.ActionView, all)
	_ = st.PutRule("alice", "old/f", protocol.ActionDownload, Rule{Mode: protocol.ModeSelected, Users: []string{"bob"}})
	_ = st.PutRule("alice", "oldx/f", protocol.ActionView, all)
	_ = st.PutRule("alice", "new/stale", protocol.ActionView, all) // устаревшее правило в месте назначения

	if err := st.RenamePrefix("alice", "old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.GetRule("alice", "old", protocol.ActionView); ok {
		t.Error("old still has a rule")
	}
	if _, ok, _ := st.GetRule("alice", "new", protocol.ActionView); !ok {
		t.Error("rule did not move to new")
	}
	got, ok, _ := st.GetRule("alice", "new/f", protocol.ActionDownload)
	if !ok || !reflect.DeepEqual(got.Users, []string{"bob"}) {
		t.Errorf("moved selected rule: %+v ok=%v", got, ok)
	}
	if _, ok, _ := st.GetRule("alice", "oldx/f", protocol.ActionView); !ok {
		t.Error("sibling oldx must be untouched")
	}
	if _, ok, _ := st.GetRule("alice", "new/stale", protocol.ActionView); ok {
		t.Error("stale rule at destination must be removed")
	}
	if err := st.RenamePrefix("alice", "new", "new/inner"); err == nil {
		t.Error("moving a folder into itself must fail")
	}
	if err := st.RenamePrefix("alice", "", "x"); err == nil {
		t.Error("renaming the root must fail")
	}

	if err := st.DeletePrefix("alice", "new"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.GetRule("alice", "new/f", protocol.ActionDownload); ok {
		t.Error("DeletePrefix left a rule behind")
	}
	if _, ok, _ := st.GetRule("alice", "oldx/f", protocol.ActionView); !ok {
		t.Error("DeletePrefix removed an unrelated rule")
	}
}

func TestSQLStoreUsage(t *testing.T) {
	st := NewSQLStore(openTestDB(t))

	if _, known, err := st.Usage("alice"); err != nil || known {
		t.Fatalf("fresh usage: known=%v err=%v", known, err)
	}
	if err := st.ReserveUsage("alice", 60, 100); err != nil {
		t.Fatal(err)
	}
	if err := st.ReserveUsage("alice", 50, 100); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}
	if n, _, _ := st.Usage("alice"); n != 60 {
		t.Errorf("usage after rejected reservation = %d, want 60", n)
	}
	if err := st.ReleaseUsage("alice", 20); err != nil {
		t.Fatal(err)
	}
	if err := st.ReserveUsage("alice", 60, 100); err != nil {
		t.Fatalf("exactly filling the quota must work: %v", err)
	}
	if n, _, _ := st.Usage("alice"); n != 100 {
		t.Errorf("usage = %d, want 100", n)
	}
	if err := st.ReleaseUsage("alice", 1000); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := st.Usage("alice"); n != 0 {
		t.Errorf("usage must not go below zero, got %d", n)
	}
	if err := st.ReserveUsage("teacher1", 1<<40, Unlimited); err != nil {
		t.Errorf("unlimited reservation failed: %v", err)
	}
}

func TestSQLStoreContactsAndPurge(t *testing.T) {
	st := NewSQLStore(openTestDB(t))
	if err := st.ReplaceContacts("alice", []string{"bob", "carol", "alice", ""}); err != nil {
		t.Fatal(err)
	}
	for contact, want := range map[string]bool{"bob": true, "carol": true, "alice": false, "dave": false} {
		if got, err := st.IsContact("alice", contact); err != nil || got != want {
			t.Errorf("IsContact(alice, %q) = %v, %v; want %v", contact, got, err, want)
		}
	}
	if err := st.ReplaceContacts("alice", []string{"dave"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.IsContact("alice", "bob"); ok {
		t.Error("ReplaceContacts must drop old contacts")
	}

	_ = st.PutRule("alice", "docs", protocol.ActionView, Rule{Mode: protocol.ModeSelected, Users: []string{"bob"}})
	_ = st.PutRule("carol", "x", protocol.ActionView, Rule{Mode: protocol.ModeSelected, Users: []string{"alice", "bob"}})
	_ = st.AddContact("bob", "alice")
	_ = st.SetUsage("alice", 5)

	if err := st.PurgeUser("alice"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.GetRule("alice", "docs", protocol.ActionView); ok {
		t.Error("owner rules must be purged")
	}
	got, _, _ := st.GetRule("carol", "x", protocol.ActionView)
	if !reflect.DeepEqual(got.Users, []string{"bob"}) {
		t.Errorf("alice must be removed as grantee, got %v", got.Users)
	}
	if ok, _ := st.IsContact("bob", "alice"); ok {
		t.Error("alice must be removed from others' contacts")
	}
	if _, known, _ := st.Usage("alice"); known {
		t.Error("usage row must be purged")
	}
}
