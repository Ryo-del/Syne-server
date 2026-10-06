package files

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCleanName(t *testing.T) {
	ok := []string{
		"report.txt", "Отчёт 1.txt", "console.txt", "com10", "a.b.c", "файл",
		strings.Repeat("a", 255),
	}
	for _, n := range ok {
		if _, err := CleanName(n); err != nil {
			t.Errorf("CleanName(%q) unexpected error: %v", n, err)
		}
	}
	bad := []string{
		"", ".", "..", "a/b", `a\b`, "con", "CON.txt", "nul.tar.gz", "com1", "LPT9.log", "COM¹",
		"name ", "name.", " name", "a:b", "a*b", "a?b", `a"b`, "a<b", "a>b", "a|b",
		"a\x00b", "a\u202eb", "a\u200bb", "a\nb", strings.Repeat("a", 256), "\xff",
	}
	for _, n := range bad {
		if _, err := CleanName(n); err == nil {
			t.Errorf("CleanName(%q) expected error", n)
		}
	}
}

func TestCleanNameNormalizesNFC(t *testing.T) {
	got, err := CleanName("e\u0301") // e + combining acute
	if err != nil {
		t.Fatal(err)
	}
	if got != "\u00e9" {
		t.Errorf("got %q, want NFC form", got)
	}
}

func TestCleanRel(t *testing.T) {
	good := map[string]string{
		"":           "",
		"a":          "a",
		"a/b/c.txt":  "a/b/c.txt",
		"e\u0301/x":  "\u00e9/x",
		"Папка/файл": "Папка/файл",
	}
	for in, want := range good {
		got, err := CleanRel(in)
		if err != nil || got != want {
			t.Errorf("CleanRel(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{"/a", "a/", "a//b", "a/../b", "./a", "a/./b", "..", `a\b`, "a/con/b"}
	for _, in := range bad {
		if _, err := CleanRel(in); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("CleanRel(%q) expected ErrInvalidPath, got %v", in, err)
		}
	}

	deep := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = "d"
		}
		return strings.Join(parts, "/")
	}
	if _, err := CleanRel(deep(32)); err != nil {
		t.Errorf("depth 32 should be allowed: %v", err)
	}
	if _, err := CleanRel(deep(33)); err == nil {
		t.Errorf("depth 33 should be rejected")
	}
}

func TestOwnerDir(t *testing.T) {
	cases := map[string]string{
		"12345678":    "12345678",
		"ivan.petrov": "ivan.petrov",
		"Ivan":        "_4976616e",
		"con":         "_636f6e",
		"../x":        "_2e2e2f78",
		"a.":          "_612e",
	}
	for login, want := range cases {
		got, err := OwnerDir(login)
		if err != nil || got != want {
			t.Errorf("OwnerDir(%q) = %q, %v; want %q", login, got, err, want)
		}
	}
	if _, err := OwnerDir(""); err == nil {
		t.Error("empty login must be rejected")
	}
	if got, err := OwnerDir(strings.Repeat("a", 100)); err != nil || len(got) != 201 {
		t.Errorf("100-char login: got len %d, err %v; want hex form of length 201", len(got), err)
	}
	if _, err := OwnerDir(strings.Repeat("a", 101)); err == nil {
		t.Error("101-byte login must be rejected")
	}
}

func TestPathHelpers(t *testing.T) {
	if got := Ancestors("a/b/c"); !reflect.DeepEqual(got, []string{"a/b/c", "a/b", "a", ""}) {
		t.Errorf("Ancestors = %v", got)
	}
	if got := Ancestors(""); !reflect.DeepEqual(got, []string{""}) {
		t.Errorf("Ancestors(root) = %v", got)
	}
	if Parent("a/b") != "a" || Parent("a") != "" || Parent("") != "" {
		t.Error("Parent")
	}
	if Base("a/b.txt") != "b.txt" || Base("x") != "x" {
		t.Error("Base")
	}
	if Join("", "x") != "x" || Join("a/b", "x") != "a/b/x" {
		t.Error("Join")
	}
	if Fold("Docs/ФАЙЛ.TXT") != "docs/файл.txt" {
		t.Errorf("Fold = %q", Fold("Docs/ФАЙЛ.TXT"))
	}
	if !EditableName("A.TXT") || EditableName("a.png") || EditableName("txt") {
		t.Error("EditableName")
	}
}

func TestResolve(t *testing.T) {
	root := t.TempDir()

	got, err := Resolve(root, "alice", "docs/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "alice", "docs", "a.txt"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got, err = Resolve(root, "alice", "")
	if err != nil || got != filepath.Join(root, "alice") {
		t.Errorf("root of owner: %q, %v", got, err)
	}

	for _, bad := range []string{"../x", "a/../../x", "/etc/passwd", `a\..\x`, "a//b"} {
		if _, err := Resolve(root, "alice", bad); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Resolve(%q) expected ErrInvalidPath, got %v", bad, err)
		}
	}
	if _, err := Resolve("", "alice", "a"); err == nil {
		t.Error("empty root must be rejected")
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alice"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "alice", "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, p := range []string{"link", "link/secret.txt"} {
		if _, err := Resolve(root, "alice", p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Resolve(%q) expected ErrInvalidPath, got %v", p, err)
		}
	}
	if _, err := Resolve(root, "alice", "newdir/file.txt"); err != nil {
		t.Errorf("non-existing path inside owner folder must be allowed: %v", err)
	}
}
