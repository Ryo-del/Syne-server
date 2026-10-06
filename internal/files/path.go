package files

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var (
	ErrInvalidPath = errors.New("invalid path")
	ErrInvalidName = errors.New("invalid name")
)

const TempPrefix = ".syne-tmp-"
const (
	maxNameBytes     = 255
	maxPathBytes     = 1024
	maxPathDepth     = 32
	maxLoginBytes    = 100
	maxSafeLoginLen  = 64
	invalidNameChars = `<>:"/\|?*`
)

// Зарезервированные имена Windows (с любым расширением).
var reservedNames = func() map[string]struct{} {
	m := map[string]struct{}{"con": {}, "prn": {}, "aux": {}, "nul": {}}
	for i := 0; i <= 9; i++ {
		m[fmt.Sprintf("com%d", i)] = struct{}{}
		m[fmt.Sprintf("lpt%d", i)] = struct{}{}
	}
	for _, s := range []string{"¹", "²", "³"} {
		m["com"+s] = struct{}{}
		m["lpt"+s] = struct{}{}
	}
	return m
}()

func isReserved(name string) bool {
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimRight(base, " ")
	_, ok := reservedNames[strings.ToLower(base)]
	return ok
}

var safeLogin = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// OwnerDir — имя папки владельца на диске.
//
// Логин используется как есть, если он безопасен на любой ОС (строчные
// латиница и цифры, '.', '_', '-', не зарезервированное имя Windows).
// Иначе имя кодируется как "_" + hex(логин). Безопасные имена никогда не
// начинаются с "_", поэтому коллизий между двумя формами нет.
func OwnerDir(login string) (string, error) {
	if login == "" || len(login) > maxLoginBytes || !utf8.ValidString(login) {
		return "", fmt.Errorf("%w: login", ErrInvalidName)
	}
	if len(login) <= maxSafeLoginLen &&
		safeLogin.MatchString(login) &&
		!strings.HasSuffix(login, ".") &&
		!isReserved(login) {
		return login, nil
	}
	return "_" + hex.EncodeToString([]byte(login)), nil
}

// CleanName проверяет одно имя файла или папки и приводит его к NFC
// (чтобы кириллица и диакритика не расходились между ОС).
func CleanName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("%w: not valid UTF-8", ErrInvalidName)
	}
	name = norm.NFC.String(name)
	switch {
	case strings.HasSuffix(name, "."):
		return "", fmt.Errorf("%w: trailing dot", ErrInvalidName)
	case strings.HasPrefix(strings.ToLower(name), TempPrefix):
		return "", fmt.Errorf("%w: reserved prefix", ErrInvalidName)
	case name == "" || name == "." || name == "..":
		return "", fmt.Errorf("%w: %q", ErrInvalidName, name)
	case len(name) > maxNameBytes:
		return "", fmt.Errorf("%w: too long", ErrInvalidName)
	case strings.TrimSpace(name) != name:
		return "", fmt.Errorf("%w: leading or trailing space", ErrInvalidName)
	case strings.HasSuffix(name, "."):
		return "", fmt.Errorf("%w: trailing dot", ErrInvalidName)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || unicode.IsControl(r) ||
			unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) ||
			strings.ContainsRune(invalidNameChars, r) {
			return "", fmt.Errorf("%w: forbidden character %q", ErrInvalidName, r)
		}
	}
	if isReserved(name) {
		return "", fmt.Errorf("%w: reserved name", ErrInvalidName)
	}
	return name, nil
}

// CleanRel проверяет относительный путь внутри папки владельца и возвращает
// его в нормальной форме. Корень — пустая строка. Путь не должен начинаться
// или заканчиваться на "/", не может содержать ".", ".." и пустых сегментов.
func CleanRel(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if !utf8.ValidString(p) || len(p) > maxPathBytes {
		return "", fmt.Errorf("%w: bad length or encoding", ErrInvalidPath)
	}
	if strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return "", fmt.Errorf("%w: leading or trailing slash", ErrInvalidPath)
	}
	segs := strings.Split(p, "/")
	if len(segs) > maxPathDepth {
		return "", fmt.Errorf("%w: too deep", ErrInvalidPath)
	}
	for i, s := range segs {
		c, err := CleanName(s)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrInvalidPath, err)
		}
		segs[i] = c
	}
	out := strings.Join(segs, "/")
	if len(out) > maxPathBytes {
		return "", fmt.Errorf("%w: too long", ErrInvalidPath)
	}
	return out, nil
}

// Fold — ключ для сравнения путей и хранения прав. Windows и macOS не
// различают регистр, поэтому "Docs/A.txt" и "docs/a.txt" — один файл и
// должны попадать под одни и те же права. Принимает путь после CleanRel.
func Fold(rel string) string {
	return strings.ToLower(norm.NFC.String(rel))
}

// Parent возвращает родительский путь ("" для элементов корня и для корня).
func Parent(rel string) string {
	i := strings.LastIndexByte(rel, '/')
	if i < 0 {
		return ""
	}
	return rel[:i]
}

// Base возвращает последний сегмент пути.
func Base(rel string) string {
	return rel[strings.LastIndexByte(rel, '/')+1:]
}
func Join(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

// Ancestors — сам путь и все его родители, заканчивая корнем (""):
// "a/b/c" -> ["a/b/c", "a/b", "a", ""].
func Ancestors(rel string) []string {
	out := []string{}
	for rel != "" {
		out = append(out, rel)
		rel = Parent(rel)
	}
	return append(out, "")
}

// EditableName — редактировать (Monaco) можно только .txt.
func EditableName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".txt")
}

// Resolve возвращает абсолютный путь на диске для (владелец, относительный
// путь) и гарантирует, что он остаётся внутри папки владельца, в том числе
// с учётом уже существующих символических ссылок и junction.
func Resolve(root, login, rel string) (string, error) {
	if root == "" {
		return "", errors.New("files: root is empty")
	}
	dir, err := OwnerDir(login)
	if err != nil {
		return "", err
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	base := filepath.Join(root, dir)
	target := base
	if clean != "" {
		target = filepath.Join(base, filepath.FromSlash(clean))
	}
	if !within(base, target) {
		return "", fmt.Errorf("%w: escapes owner folder", ErrInvalidPath)
	}
	if err := checkNoEscape(base, target); err != nil {
		return "", err
	}
	return target, nil
}

func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(rel)
}

// checkNoEscape проверяет ближайший существующий предок target: после
// раскрытия символических ссылок он должен остаться внутри папки владельца.
func checkNoEscape(base, target string) error {
	existing := target
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return nil
		}
		existing = parent
	}
	if !within(base, existing) {
		return nil // внутри папки владельца ещё ничего нет
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return fmt.Errorf("%w: cannot verify folder", ErrInvalidPath)
	}
	realExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return fmt.Errorf("%w: cannot verify path", ErrInvalidPath)
	}
	if !within(realBase, realExisting) {
		return fmt.Errorf("%w: escapes owner folder", ErrInvalidPath)
	}
	return nil
}
