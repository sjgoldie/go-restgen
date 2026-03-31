//nolint:staticcheck,errcheck,gosec // Test code - string context keys and unchecked test cleanup are acceptable
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sjgoldie/go-restgen/filestore"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
)

func TestDownload_ContentDisposition_SafeEncoding(t *testing.T) {
	tests := []struct {
		name           string
		filename       string
		expectedHeader string
	}{
		{
			"normal filename",
			"test.txt",
			`attachment; filename=test.txt`,
		},
		{
			"filename with spaces gets quoted",
			"my file.txt",
			`attachment; filename="my file.txt"`,
		},
		{
			"filename with quotes",
			`evil".html`,
			`attachment; filename="evil\".html"`,
		},
		{
			"non-ASCII filename uses RFC 2231",
			"café.pdf",
			"attachment; filename*=utf-8''caf%C3%A9.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
			if err != nil {
				t.Fatal("Failed to create table:", err)
			}
			defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

			testFileStorage.files["test-key"] = "content"
			defer delete(testFileStorage.files, "test-key")

			file := &TestFileModel{
				Name: "Test",
				FileFields: filestore.FileFields{
					StorageKey:  "test-key",
					Filename:    tt.filename,
					ContentType: "text/plain",
					Size:        7,
				},
			}
			_, err = testDB.GetDB().NewInsert().Model(file).Returning("*").Exec(context.Background())
			if err != nil {
				t.Fatal("Failed to create file:", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/files/1/download", nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add(testFileMeta.URLParamUUID, "1")
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
			ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.Download[TestFileModel]()(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
			}

			cd := w.Header().Get("Content-Disposition")
			if cd != tt.expectedHeader {
				t.Errorf("Content-Disposition = %q, want %q", cd, tt.expectedHeader)
			}
		})
	}
}

func TestCreate_MultipartUpload_SanitizesFilename(t *testing.T) {
	tests := []struct {
		name             string
		uploadFilename   string
		expectedFilename string
	}{
		{"normal filename", "photo.png", "photo.png"},
		{"path traversal", "../../etc/passwd", "passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testDB.GetDB().NewCreateTable().Model((*TestFileModel)(nil)).IfNotExists().Exec(context.Background())
			if err != nil {
				t.Fatal("Failed to create table:", err)
			}
			defer testDB.GetDB().NewDropTable().Model((*TestFileModel)(nil)).IfExists().Exec(context.Background())

			var body bytes.Buffer
			writer := multipart.NewWriter(&body)

			fileWriter, err := writer.CreateFormFile("file", tt.uploadFilename)
			if err != nil {
				t.Fatal("Failed to create form file:", err)
			}
			fileWriter.Write([]byte("file content"))

			metadataField, err := writer.CreateFormField("metadata")
			if err != nil {
				t.Fatal("Failed to create metadata field:", err)
			}
			metadataField.Write([]byte(`{"name":"Test"}`))

			writer.Close()

			req := httptest.NewRequest(http.MethodPost, "/files", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())

			rctx := chi.NewRouteContext()
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
			ctx = context.WithValue(ctx, metadata.MetadataKey, testFileMeta)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.Create(handler.StandardCreate[TestFileModel])(w, req)

			if w.Code != http.StatusCreated {
				t.Fatalf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
			}

			var result TestFileModel
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if result.Filename != tt.expectedFilename {
				t.Errorf("Filename = %q, want %q", result.Filename, tt.expectedFilename)
			}
		})
	}
}
