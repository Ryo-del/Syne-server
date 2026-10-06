package files

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"server/internal/db"

	protocol "github.com/Ryo-del/Syne-protocol"
)

var (
	uAlice = Actor{Login: "alice", Role: "student"}
	uBob   = Actor{Login: "bob", Role: "student"}
	uCarol = Actor{Login: "carol", Role: "student"}
	uTeach = Actor{Login: "teach", Role: "teacher"}
)

type svcEnv struct {
	t     *testing.T
	svc   *Service
	root  string
	quota int64
}

func newSvcEnv(t *testing.T) *svcEnv {
	t.Helper()
	d := openTestDB(t)
	if err := db.InitSchema(d); err != nil { // users, sessions и т.д.
		t.Fatal(err)
	}
	users := []struct{ login, fname, sname, role string }{
		{"alice", "Алиса", "Иванова", "student"},
		{"bob", "Борис", "Петров", "student"},
		{"carol", "Карина", "Сидорова", "student"},
		{"teach", "Тимур", "Учителев", "teacher"},
	}
	for _, u := range users {
		if err := db.CreateClaimableUser(d, u.login, u.fname, u.sname, u.role, true, "CODE1234", []byte{1}); err != nil {
			t.Fatal(err)
		}
	}
	e := &svcEnv{t: t, root: t.TempDir(), quota: Unlimited}
	svc, err := NewService(d, e.root, func() int64 { return e.quota })
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

func rq(op, owner, path string) protocol.FilesRequest {
	return protocol.FilesRequest{Op: op, Owner: owner, Path: path}
}

func (e *svcEnv) ok(a Actor, req protocol.FilesRequest) protocol.FilesResponse {
	e.t.Helper()
	r := e.svc.Do(a, req)
	if !r.OK {
		e.t.Fatalf("%s %q as %s: %s (%s)", req.Op, req.Path, a.Login, r.Error, r.Code)
	}
	return r
}

func (e *svcEnv) fail(a Actor, req protocol.FilesRequest, code string) {
	e.t.Helper()
	r := e.svc.Do(a, req)
	if r.OK || r.Code != code {
		e.t.Errorf("%s %q as %s: got ok=%v code=%q, want code %q", req.Op, req.Path, a.Login, r.OK, r.Code, code)
	}
}

func (e *svcEnv) rule(by Actor, owner, path string, act protocol.Action, mode protocol.Mode, users ...string) {
	e.t.Helper()
	req := rq(protocol.FilesOpRuleSet, owner, path)
	req.Action, req.Mode, req.Users = act, mode, users
	e.ok(by, req)
}

func (e *svcEnv) abs(owner, rel string) string {
	return filepath.Join(e.root, owner, filepath.FromSlash(rel))
}

func (e *svcEnv) write(owner, rel, content string) {
	e.t.Helper()
	p := e.abs(owner, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *svcEnv) read(owner, rel string) string {
	e.t.Helper()
	b, err := os.ReadFile(e.abs(owner, rel))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func (e *svcEnv) exists(owner, rel string) bool {
	_, err := os.Stat(e.abs(owner, rel))
	return err == nil
}

// names сверяет имена элементов списка (в порядке ответа).
func (e *svcEnv) names(r protocol.FilesResponse, want ...string) {
	e.t.Helper()
	got := make([]string, 0, len(r.Entries))
	for _, en := range r.Entries {
		got = append(got, en.Name)
	}
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		e.t.Errorf("entries = %v, want %v", got, want)
	}
}

func TestServiceListHidesWhatYouCannotSee(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "docs/a.txt", "hello")
	e.write("alice", "docs/secret.txt", "x")
	e.write("alice", "private.txt", "y")

	// пока ничего не открыто, ученик не видит чужих папок
	e.names(e.ok(uBob, rq(protocol.FilesOpListOwners, "", "")))

	e.rule(uAlice, "", "docs/a.txt", protocol.ActionView, protocol.ModeAll)

	owners := e.ok(uBob, rq(protocol.FilesOpListOwners, "", ""))
	if len(owners.Entries) != 1 || owners.Entries[0].Owner != "alice" || owners.Entries[0].Name != "Иванова Алиса" {
		t.Fatalf("owners = %+v", owners.Entries)
	}
	e.names(e.ok(uBob, rq(protocol.FilesOpList, "alice", "")), "docs")
	listing := e.ok(uBob, rq(protocol.FilesOpList, "alice", "docs"))
	e.names(listing, "a.txt")
	can := listing.Entries[0].Can
	if !can[protocol.ActionView] || can[protocol.ActionDownload] || !can[protocol.CapOpen] {
		t.Errorf("Can = %v", can)
	}

	// скрытое неотличимо от несуществующего
	e.fail(uBob, rq(protocol.FilesOpStat, "alice", "docs/secret.txt"), protocol.FilesErrNotFound)
	e.fail(uBob, rq(protocol.FilesOpStat, "alice", "private.txt"), protocol.FilesErrNotFound)
	e.fail(uBob, rq(protocol.FilesOpStat, "alice", "nope.txt"), protocol.FilesErrNotFound)
	e.fail(uBob, rq(protocol.FilesOpList, "alice", "../bob"), protocol.FilesErrInvalidPath)

	// преподаватель видит всё и всех, кроме себя
	e.names(e.ok(uTeach, rq(protocol.FilesOpListOwners, "", "")), "Иванова Алиса", "Петров Борис", "Сидорова Карина")
	e.names(e.ok(uTeach, rq(protocol.FilesOpList, "alice", "docs")), "a.txt", "secret.txt")

	// владелец: сначала папки, потом файлы
	e.names(e.ok(uAlice, rq(protocol.FilesOpList, "", "")), "docs", "private.txt")
}

func TestServiceCreateRenameDelete(t *testing.T) {
	e := newSvcEnv(t)
	mk := func(op, parent, name string) protocol.FilesRequest {
		r := rq(op, "", parent)
		r.Name = name
		return r
	}

	e.ok(uAlice, mk(protocol.FilesOpMkdir, "", "Docs"))
	e.fail(uAlice, mk(protocol.FilesOpMkdir, "", "docs"), protocol.FilesErrExists)
	e.ok(uAlice, mk(protocol.FilesOpCreate, "Docs", "a.txt"))
	e.fail(uAlice, mk(protocol.FilesOpCreate, "Docs", "A.TXT"), protocol.FilesErrExists)
	e.fail(uAlice, mk(protocol.FilesOpCreate, "Docs", "con"), protocol.FilesErrInvalidPath)
	e.fail(uAlice, mk(protocol.FilesOpCreate, "Docs", "a/b"), protocol.FilesErrInvalidPath)
	e.fail(uAlice, mk(protocol.FilesOpCreate, "Docs", ".syne-tmp-x"), protocol.FilesErrInvalidPath)

	r := e.ok(uAlice, mk(protocol.FilesOpRename, "Docs/a.txt", "b.txt"))
	if r.Entry == nil || r.Entry.Path != "Docs/b.txt" {
		t.Fatalf("rename entry = %+v", r.Entry)
	}
	if !e.exists("alice", "Docs/b.txt") || e.exists("alice", "Docs/a.txt") {
		t.Fatal("rename did not move the file")
	}

	// права переезжают вместе с файлом, в том числе при смене только регистра
	e.rule(uAlice, "", "Docs/b.txt", protocol.ActionView, protocol.ModeAll)
	e.ok(uAlice, mk(protocol.FilesOpRename, "Docs/b.txt", "c.txt"))
	e.ok(uBob, rq(protocol.FilesOpStat, "alice", "Docs/c.txt"))
	e.ok(uAlice, mk(protocol.FilesOpRename, "Docs/c.txt", "C.TXT"))
	e.ok(uBob, rq(protocol.FilesOpStat, "alice", "Docs/C.TXT"))

	// после удаления права не должны достаться новому файлу с тем же именем
	e.ok(uAlice, rq(protocol.FilesOpDelete, "", "Docs"))
	e.fail(uBob, rq(protocol.FilesOpStat, "alice", "Docs/C.TXT"), protocol.FilesErrNotFound)
	e.ok(uAlice, mk(protocol.FilesOpMkdir, "", "Docs"))
	e.ok(uAlice, mk(protocol.FilesOpCreate, "Docs", "c.txt"))
	e.fail(uBob, rq(protocol.FilesOpStat, "alice", "Docs/c.txt"), protocol.FilesErrNotFound)

	e.fail(uAlice, rq(protocol.FilesOpDelete, "", ""), protocol.FilesErrInvalidRequest)
}

func TestServiceRenameDeletePermissions(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "shared/a.txt", "a")
	e.write("alice", "shared/pic.png", "p")
	e.write("alice", "shared/sub/ok.txt", "1")
	e.write("alice", "shared/sub/locked.txt", "2")
	for _, act := range []protocol.Action{protocol.ActionView, protocol.ActionEdit, protocol.ActionDelete} {
		e.rule(uAlice, "", "shared", act, protocol.ModeAll)
	}
	e.rule(uAlice, "", "shared/sub/locked.txt", protocol.ActionDelete, protocol.ModeNone)

	ren := func(path, name string) protocol.FilesRequest {
		r := rq(protocol.FilesOpRename, "alice", path)
		r.Name = name
		return r
	}
	// чужие файлы: только .txt -> .txt
	e.ok(uBob, ren("shared/a.txt", "c.txt"))
	e.fail(uBob, ren("shared/c.txt", "c.png"), protocol.FilesErrForbidden)
	e.fail(uBob, ren("shared/pic.png", "x.png"), protocol.FilesErrForbidden)
	e.ok(uTeach, ren("shared/pic.png", "x.png"))
	// папку переименовать можно, правило на вложенном файле идёт следом
	e.ok(uBob, ren("shared/sub", "sub2"))

	// удаление папки: всё или ничего
	e.fail(uBob, rq(protocol.FilesOpDelete, "alice", "shared/sub2"), protocol.FilesErrForbidden)
	e.fail(uBob, rq(protocol.FilesOpDelete, "alice", "shared/sub2/locked.txt"), protocol.FilesErrForbidden)
	if !e.exists("alice", "shared/sub2/ok.txt") || !e.exists("alice", "shared/sub2/locked.txt") {
		t.Fatal("failed delete must not remove anything")
	}
	e.ok(uBob, rq(protocol.FilesOpDelete, "alice", "shared/sub2/ok.txt"))
	e.ok(uBob, rq(protocol.FilesOpDelete, "alice", "shared/c.txt"))
	if e.exists("alice", "shared/c.txt") || e.exists("alice", "shared/sub2/ok.txt") {
		t.Fatal("files were not deleted")
	}
}

func TestServiceCopyPasteDuplicate(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "docs/a.txt", "hello")
	e.write("alice", "docs/f/x.txt", "xxx")
	e.write("alice", "docs/f/y.txt", "yy")
	e.rule(uAlice, "", "docs", protocol.ActionView, protocol.ModeAll)
	e.rule(uAlice, "", "docs", protocol.ActionCopy, protocol.ModeAll)
	e.rule(uAlice, "", "docs/f/y.txt", protocol.ActionView, protocol.ModeNone)

	// назначение не задано: корень папки самого пользователя
	paste := func(path, policy string) protocol.FilesRequest {
		r := rq(protocol.FilesOpPaste, "alice", path)
		r.OnConflict = policy
		return r
	}

	r := e.ok(uBob, paste("docs/a.txt", ""))
	if r.Entry == nil || r.Entry.Owner != "bob" || r.Entry.Name != "a.txt" {
		t.Fatalf("paste entry = %+v", r.Entry)
	}
	if e.read("bob", "a.txt") != "hello" {
		t.Fatal("copy content mismatch")
	}

	e.fail(uBob, paste("docs/a.txt", ""), protocol.FilesErrExists)
	r = e.ok(uBob, paste("docs/a.txt", protocol.ConflictRename))
	if r.Entry == nil || r.Entry.Name != "a (1).txt" {
		t.Fatalf("rename policy entry = %+v", r.Entry)
	}
	r = e.ok(uBob, paste("docs/a.txt", protocol.ConflictSkip))
	if r.Entry != nil || r.Skipped != 1 {
		t.Fatalf("skip policy response = %+v", r)
	}
	e.write("bob", "a.txt", "old")
	e.ok(uBob, paste("docs/a.txt", protocol.ConflictReplace))
	if e.read("bob", "a.txt") != "hello" {
		t.Fatal("replace did not overwrite")
	}

	// папка: скрытый файл не копируется, число пропущенных возвращается
	r = e.ok(uBob, paste("docs/f", ""))
	if r.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", r.Skipped)
	}
	if e.read("bob", "f/x.txt") != "xxx" || e.exists("bob", "f/y.txt") {
		t.Error("folder copy must contain only permitted files")
	}

	// нельзя копировать папку в саму себя
	into := rq(protocol.FilesOpPaste, "", "docs")
	into.DestPath = "docs/f"
	e.fail(uAlice, into, protocol.FilesErrInvalidRequest)

	// дубликат рядом с оригиналом
	dup := rq(protocol.FilesOpDuplicate, "", "a.txt")
	if r = e.ok(uBob, dup); r.Entry == nil || r.Entry.Name != "a (копия).txt" {
		t.Fatalf("first duplicate = %+v", r.Entry)
	}
	if r = e.ok(uBob, dup); r.Entry == nil || r.Entry.Name != "a (копия 2).txt" {
		t.Fatalf("second duplicate = %+v", r.Entry)
	}
	// в чужой папке нужно ещё и право вставки
	e.fail(uBob, rq(protocol.FilesOpDuplicate, "alice", "docs/a.txt"), protocol.FilesErrForbidden)
}

func TestServiceCopyQuota(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "docs/a.txt", "hello") // 5 байт
	e.write("alice", "docs/b.txt", "abc")   // 3 байта
	e.rule(uAlice, "", "docs", protocol.ActionView, protocol.ModeAll)
	e.rule(uAlice, "", "docs", protocol.ActionCopy, protocol.ModeAll)
	e.quota = 6

	e.ok(uCarol, rq(protocol.FilesOpPaste, "alice", "docs/a.txt"))
	e.fail(uCarol, rq(protocol.FilesOpPaste, "alice", "docs/b.txt"), protocol.FilesErrQuota)
	if e.exists("carol", "b.txt") {
		t.Error("file must not be created when the quota is exceeded")
	}

	q := e.ok(uCarol, rq(protocol.FilesOpQuota, "", "")).Quota
	if q == nil || q.UsedBytes != 5 || q.LimitBytes != 6 || q.Unlimited {
		t.Errorf("quota = %+v", q)
	}
	e.fail(uBob, rq(protocol.FilesOpQuota, "carol", ""), protocol.FilesErrForbidden)
	if q := e.ok(uTeach, rq(protocol.FilesOpQuota, "carol", "")).Quota; q == nil || q.UsedBytes != 5 {
		t.Errorf("teacher view of quota = %+v", q)
	}
}

func TestServiceFavorites(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "docs/a.txt", "x")
	e.rule(uAlice, "", "docs", protocol.ActionView, protocol.ModeAll)

	favList := rq(protocol.FilesOpFavList, "", "")
	e.ok(uBob, rq(protocol.FilesOpFavAdd, "alice", "docs/a.txt"))
	e.names(e.ok(uBob, favList), "a.txt")

	// ссылка следует за переименованием
	ren := rq(protocol.FilesOpRename, "", "docs/a.txt")
	ren.Name = "b.txt"
	e.ok(uAlice, ren)
	e.names(e.ok(uBob, favList), "b.txt")

	// доступ закрыли: ссылка пропадает и не возвращается сама
	e.rule(uAlice, "", "docs", protocol.ActionView, protocol.ModeNone)
	e.names(e.ok(uBob, favList))
	e.rule(uAlice, "", "docs", protocol.ActionView, protocol.ModeAll)
	e.names(e.ok(uBob, favList))
}

func TestServiceStateContactsAndPurge(t *testing.T) {
	e := newSvcEnv(t)

	put := rq(protocol.FilesOpUIPut, "", "")
	put.Data = `{"hidden":["server"]}`
	e.ok(uBob, put)
	if got := e.ok(uBob, rq(protocol.FilesOpUIGet, "", "")).Data; got != put.Data {
		t.Errorf("ui state = %q", got)
	}
	put.Data = "not json"
	e.fail(uBob, put, protocol.FilesErrInvalidRequest)

	// «только контактам» — контакты владельца
	e.write("alice", "docs/a.txt", "hello")
	e.rule(uAlice, "", "docs", protocol.ActionView, protocol.ModeContacts)
	stat := rq(protocol.FilesOpStat, "alice", "docs/a.txt")
	e.fail(uBob, stat, protocol.FilesErrNotFound)
	cp := rq(protocol.FilesOpContactsPut, "", "")
	cp.Contacts = []string{"bob"}
	e.ok(uAlice, cp)
	e.ok(uBob, stat)
	e.fail(uCarol, stat, protocol.FilesErrNotFound)

	// права видны только владельцу и преподавателю; неизвестный пользователь в списке — ошибка
	e.fail(uBob, rq(protocol.FilesOpRulesGet, "alice", "docs"), protocol.FilesErrForbidden)
	if rules := e.ok(uAlice, rq(protocol.FilesOpRulesGet, "", "docs")).Rules; len(rules) == 0 {
		t.Error("rules_get returned nothing")
	}
	bad := rq(protocol.FilesOpRuleSet, "", "docs")
	bad.Action, bad.Mode, bad.Users = protocol.ActionView, protocol.ModeSelected, []string{"ghost"}
	e.fail(uAlice, bad, protocol.FilesErrInvalidRequest)

	// удаление пользователя стирает папку и правила
	if err := e.svc.PurgeUser("alice"); err != nil {
		t.Fatal(err)
	}
	if e.exists("alice", "") {
		t.Error("user folder must be removed")
	}
	if _, found, _ := e.svc.st.GetRule("alice", "docs", protocol.ActionView); found {
		t.Error("rules must be removed")
	}
}
