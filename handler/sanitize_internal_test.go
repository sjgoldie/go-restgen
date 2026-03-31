package handler

import (
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal filename", "report.pdf", "report.pdf"},
		{"path traversal unix", "../../etc/passwd", "passwd"},
		{"path traversal windows", `..\..\windows\system32\config`, "config"},
		{"directory prefix", "/var/uploads/file.txt", "file.txt"},
		{"null byte", "file\x00name.txt", "filename.txt"},
		{"newline", "file\nname.txt", "filename.txt"},
		{"carriage return", "file\rname.txt", "filename.txt"},
		{"tab", "file\tname.txt", "filename.txt"},
		{"DEL character", "file\x7fname.txt", "filename.txt"},
		{"mixed control chars", "a\x01b\x02c\x03.txt", "abc.txt"},
		{"double quotes preserved", `evil".html`, `evil".html`},
		{"non-ASCII UTF-8", "café report.pdf", "café report.pdf"},
		{"CJK characters", "日本語.pdf", "日本語.pdf"},
		{"emoji", "📊 data.xlsx", "📊 data.xlsx"},
		{"empty string", "", ""},
		{"dot only", ".", ""},
		{"double dot", "..", ""},
		{"whitespace only", "   ", ""},
		{"leading trailing spaces", "  file.txt  ", "file.txt"},
		{"hidden file", ".hidden", ".hidden"},
		{"mixed attack", "../../../\x00evil\r\nfile.txt", "evilfile.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestContentDisposition(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected string
	}{
		{"simple filename", "test.txt", `attachment; filename=test.txt`},
		{"filename with spaces", "my file.txt", `attachment; filename="my file.txt"`},
		{"filename with quotes", `evil".html`, `attachment; filename="evil\".html"`},
		{"non-ASCII uses RFC 2231", "café.pdf", "attachment; filename*=utf-8''caf%C3%A9.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contentDisposition(tt.filename)
			if got != tt.expected {
				t.Errorf("contentDisposition(%q) = %q, want %q", tt.filename, got, tt.expected)
			}
		})
	}
}
