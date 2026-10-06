package files

import (
	"archive/zip"
	"bytes"
	"io"
	"reflect"
	"sort"
	"testing"

	protocol "github.com/Ryo-del/Syne-protocol"
)

func (e *svcEnv) search(a Actor, query string, content bool, limit int) protocol.FilesResponse {
	e.t.Helper()
	return e.ok(a, protocol.FilesRequest{Op: protocol.FilesOpSearch, Query: query, Content: content, Limit: limit})
}

func hits(r protocol.FilesResponse) []string {
	out := make([]string, 0, len(r.Entries))
	for _, en := range r.Entries {
		out = append(out, en.Owner+":"+en.Path)
	}
	sort.Strings(out)
	return out
}

func wantHits(t *testing.T, what string, r protocol.FilesResponse, want ...string) {
	t.Helper()
	got := hits(r)
	sort.Strings(want)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: hits = %v, want %v", what, got, want)
	}
}

func TestServiceSearch(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "docs/Report.txt", "Hello World\nsecond line")
	e.write("alice", "docs/notes.txt", "nothing here")
	e.write("alice", "docs/secret.txt", "hello secret")
	e.write("alice", "other/img.png", "png")
	e.write("bob", "mine.txt", "my own file")
	e.rule(uAlice, "", "docs", protocol.ActionView, protocol.ModeAll)
	e.rule(uAlice, "", "docs/secret.txt", protocol.ActionView, protocol.ModeNone)

	// по имени; скрытое не находится
	wantHits(t, "by name", e.search(uBob, "report", false, 0), "alice:docs/Report.txt")
	wantHits(t, "folder name", e.search(uBob, "docs", false, 0), "alice:docs")
	wantHits(t, "hidden by name", e.search(uBob, "secret", false, 0))

	// по содержимому
	wantHits(t, "content search off", e.search(uBob, "hello", false, 0))
	r := e.search(uBob, "hello", true, 0)
	wantHits(t, "content search on", r, "alice:docs/Report.txt")
	if r.Entries[0].Snippet != "Hello World" {
		t.Errorf("snippet = %q", r.Entries[0].Snippet)
	}
	both := []string{"alice:docs/Report.txt", "alice:docs/secret.txt"}
	wantHits(t, "owner sees hidden", e.search(uAlice, "hello", true, 0), both...)
	wantHits(t, "teacher sees hidden", e.search(uTeach, "hello", true, 0), both...)

	// свои файлы и область поиска
	wantHits(t, "own files", e.search(uBob, "mine", false, 0), "bob:mine.txt")
	scoped := protocol.FilesRequest{Op: protocol.FilesOpSearch, Query: "txt", Owner: "alice"}
	wantHits(t, "scoped to one owner", e.ok(uBob, scoped), "alice:docs/Report.txt", "alice:docs/notes.txt")

	// лимит
	lim := e.search(uAlice, "txt", false, 2)
	if len(lim.Entries) != 2 || !lim.Truncated {
		t.Errorf("limit: %d entries, truncated=%v", len(lim.Entries), lim.Truncated)
	}

	e.fail(uBob, protocol.FilesRequest{Op: protocol.FilesOpSearch, Query: "  "}, protocol.FilesErrInvalidRequest)
}

func TestServiceSearchUnicode(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "Заметка.txt", "Привет МИР")
	wantHits(t, "cyrillic name", e.search(uAlice, "ЗАМЕТ", false, 0), "alice:Заметка.txt")
	r := e.search(uAlice, "привет мир", true, 0)
	wantHits(t, "cyrillic content", r, "alice:Заметка.txt")
	if r.Entries[0].Snippet != "Привет МИР" {
		t.Errorf("snippet = %q", r.Entries[0].Snippet)
	}
}

func TestServiceSend(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "docs/a.txt", "hello")
	e.write("alice", "docs/b.txt", "bbb")
	e.rule(uAlice, "", "docs", protocol.ActionView, protocol.ModeAll)
	e.rule(uAlice, "", "docs", protocol.ActionSend, protocol.ModeAll)
	e.rule(uAlice, "", "docs/b.txt", protocol.ActionSend, protocol.ModeNone)

	send := func(owner, path, to string) protocol.FilesRequest {
		r := rq(protocol.FilesOpSend, owner, path)
		r.DestOwner = to
		return r
	}

	r := e.ok(uBob, send("alice", "docs/a.txt", "carol"))
	if r.Entry == nil || r.Entry.Owner != "carol" || r.Entry.Path != "Полученные/a.txt" {
		t.Fatalf("send entry = %+v", r.Entry)
	}
	if e.read("carol", "Полученные/a.txt") != "hello" || e.usage("carol") != 5 {
		t.Errorf("content %q, recipient usage %d", e.read("carol", "Полученные/a.txt"), e.usage("carol"))
	}
	r = e.ok(uBob, send("alice", "docs/a.txt", "carol"))
	if r.Entry == nil || r.Entry.Path != "Полученные/a (1).txt" {
		t.Fatalf("second send entry = %+v", r.Entry)
	}

	e.fail(uBob, send("alice", "docs/b.txt", "carol"), protocol.FilesErrForbidden)
	e.fail(uBob, send("alice", "docs/a.txt", "bob"), protocol.FilesErrInvalidRequest)
	e.fail(uBob, send("alice", "docs/a.txt", "ghost"), protocol.FilesErrNotFound)
	e.fail(uBob, send("alice", "docs", "carol"), protocol.FilesErrInvalidRequest)

	// владельцу право send не нужно
	e.ok(uAlice, send("", "docs/b.txt", "bob"))
	if e.read("bob", "Полученные/b.txt") != "bbb" {
		t.Error("owner send content mismatch")
	}
}

func TestServiceSendLimits(t *testing.T) {
	e := newSvcEnv(t)
	e.write("bob", "a.bin", "12345")
	e.write("bob", "b.bin", "12345")
	send := func(path string) protocol.FilesRequest {
		r := rq(protocol.FilesOpSend, "", path)
		r.DestOwner = "carol"
		return r
	}

	// квота получателя
	e.quota = 8
	e.ok(uBob, send("a.bin"))
	e.fail(uBob, send("b.bin"), protocol.FilesErrQuota)
	if e.exists("carol", "Полученные/b.bin") {
		t.Error("file must not be delivered over the recipient's quota")
	}

	// потолок папки «Полученные»
	e.quota = Unlimited
	old := maxReceivedBytes
	maxReceivedBytes = 7
	defer func() { maxReceivedBytes = old }()
	e.fail(uBob, send("b.bin"), protocol.FilesErrQuota)
	if e.exists("carol", "Полученные/b.bin") {
		t.Error("file must not be delivered over the inbox cap")
	}
}

func zipNames(t *testing.T, b []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

func TestServiceDownloadDirZip(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "docs/a.txt", "aaa")
	e.write("alice", "docs/f/b.txt", "bb")
	e.write("alice", "docs/f/hid.txt", "h")
	e.write("alice", "other/x.txt", "x")
	e.rule(uAlice, "", "docs", protocol.ActionView, protocol.ModeAll)
	e.rule(uAlice, "", "docs", protocol.ActionDownload, protocol.ModeAll)
	e.rule(uAlice, "", "docs/f/hid.txt", protocol.ActionDownload, protocol.ModeNone)
	e.rule(uAlice, "", "other", protocol.ActionView, protocol.ModeAll)

	job, entry, err := e.svc.PrepareDirZip(uBob, rq(protocol.FilesOpDownloadDir, "alice", "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != "docs.zip" || job.Skipped != 1 {
		t.Errorf("entry.Name = %q, skipped = %d", entry.Name, job.Skipped)
	}
	var buf bytes.Buffer
	if err := job.WriteZip(&buf); err != nil {
		t.Fatal(err)
	}
	want := []string{"docs/", "docs/a.txt", "docs/f/", "docs/f/b.txt"}
	if got := zipNames(t, buf.Bytes()); !reflect.DeepEqual(got, want) {
		t.Errorf("zip entries = %v, want %v", got, want)
	}
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	for _, f := range zr.File {
		if f.Name != "docs/f/b.txt" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		if string(data) != "bb" {
			t.Errorf("zipped content = %q", data)
		}
	}

	// без права скачивания на папке
	_, _, err = e.svc.PrepareDirZip(uBob, rq(protocol.FilesOpDownloadDir, "alice", "other"))
	wantCode(t, "zip without download", err, protocol.FilesErrForbidden)

	// вся папка владельца: имя архива и корень внутри — «Фамилия Имя»
	job, entry, err = e.svc.PrepareDirZip(uAlice, rq(protocol.FilesOpDownloadDir, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != "Иванова Алиса.zip" {
		t.Errorf("entry.Name = %q", entry.Name)
	}
	buf.Reset()
	if err := job.WriteZip(&buf); err != nil {
		t.Fatal(err)
	}
	names := zipNames(t, buf.Bytes())
	found := false
	for _, n := range names {
		if n == "Иванова Алиса/other/x.txt" {
			found = true
		}
	}
	if len(names) == 0 || names[0] != "Иванова Алиса/" || !found {
		t.Errorf("owner zip entries = %v", names)
	}
}
func TestServiceSendBlocked(t *testing.T) {
	e := newSvcEnv(t)
	e.write("bob", "a.txt", "hi")
	send := rq(protocol.FilesOpSend, "", "a.txt")
	send.DestOwner = "carol"

	bl := rq(protocol.FilesOpBlockedPut, "", "")
	bl.Blocked = []string{"bob"}
	e.ok(uCarol, bl)

	// отправитель видит «успех», но файл не доставлен и место не занято
	if r := e.ok(uBob, send); r.Entry == nil {
		t.Fatal("blocked send must look like a success")
	}
	if e.exists("carol", "Полученные/a.txt") || e.usage("carol") != 0 {
		t.Error("blocked sender must not deliver files")
	}

	// после разблокировки доставка работает
	bl.Blocked = nil
	e.ok(uCarol, bl)
	e.ok(uBob, send)
	if e.read("carol", "Полученные/a.txt") != "hi" {
		t.Error("send after unblock failed")
	}
}
