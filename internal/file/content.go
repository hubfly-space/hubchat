package file

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

// Validate signatures for recognized binary formats instead of trusting the
// multipart MIME string. Text formats remain useful for support attachments.
func validateUploadContent(upload Upload, header []byte) error {
	detected, _, _ := mime.ParseMediaType(http.DetectContentType(header))
	declared, _, err := mime.ParseMediaType(upload.MIMEType)
	if err != nil && upload.MIMEType != "" {
		return ErrMimeNotAllowed
	}
	binary := map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "application/pdf": true, "application/zip": true, "application/x-gzip": true}
	expected := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".pdf": "application/pdf", ".zip": "application/zip", ".gz": "application/x-gzip", ".docx": "application/zip", ".xlsx": "application/zip", ".pptx": "application/zip"}
	if format := expected[strings.ToLower(filepath.Ext(upload.Name))]; format != "" && detected != format {
		return ErrMimeNotAllowed
	}
	if binary[declared] && detected != declared {
		return ErrMimeNotAllowed
	}
	if strings.HasPrefix(declared, "image/") && declared != "image/svg+xml" && !strings.HasPrefix(detected, "image/") {
		return ErrMimeNotAllowed
	}
	return nil
}
