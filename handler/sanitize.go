package handler

import (
	"mime"
	"path/filepath"
	"strings"
)

// sanitizeFilename cleans an untrusted filename for safe use in storage backends
// and HTTP headers. It strips path components (preventing path traversal),
// removes control characters, and trims whitespace.
// Non-ASCII characters (UTF-8) are preserved.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	if name == "." || name == ".." {
		return ""
	}

	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)

	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	return name
}

// contentDisposition builds a safe Content-Disposition header value using
// mime.FormatMediaType, which handles quoting for ASCII filenames and
// RFC 2231 encoding for non-ASCII filenames.
func contentDisposition(filename string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": filename})
}
