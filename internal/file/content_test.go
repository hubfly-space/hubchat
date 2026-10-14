package file

import "testing"

func TestContentRejectsDisguisedBinaryUploads(t *testing.T) {
	if err := validateUploadContent(Upload{Name: "picture.png", MIMEType: "image/png"}, []byte("%PDF-1.7\n")); err != ErrMimeNotAllowed {
		t.Fatalf("disguised PNG: %v", err)
	}
	if err := validateUploadContent(Upload{Name: "notes.txt", MIMEType: "text/plain"}, []byte("support notes")); err != nil {
		t.Fatal(err)
	}
	if err := validateUploadContent(Upload{Name: "data.csv", MIMEType: "text/csv"}, []byte("name,email\nAda,ada@example.test")); err != nil {
		t.Fatal(err)
	}
}
