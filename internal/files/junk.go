package files

import "strings"

// IsJunkName — служебные имена macOS/Windows. Они не показываются, не ищутся и не принимаются.
func IsJunkName(name string) bool {
	n := strings.ToLower(name)
	switch n {
	case ".ds_store", ".appledouble", "__macosx", "thumbs.db", "desktop.ini",
		".spotlight-v100", ".trashes", ".fseventsd", ".temporaryitems", ".documentrevisions-v100":
		return true
	}
	return strings.HasPrefix(n, "._")
}
