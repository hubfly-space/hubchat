package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerFileRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("POST /v1/files",
		Idempotency(deps)(requireCapability(deps, authorization.ConversationReply, handleUploadFile(deps))))
	mux.HandleFunc("GET /v1/files/{id}",
		requireActor(deps, handleDownloadFile(deps)))
}

func handleUploadFile(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		// Keep multipart metadata in memory small; MaxBytes middleware still
		// bounds the complete request body, including the file bytes.
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "The upload could not be read.")
			return
		}
		parts := r.MultipartForm.File["file"]
		if len(parts) != 1 {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Upload exactly one file.")
			return
		}
		part := parts[0]
		opened, err := part.Open()
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "The upload could not be opened.")
			return
		}
		defer opened.Close()

		mimeType := part.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = mime.TypeByExtension(filepath.Ext(part.Filename))
		}
		created, err := deps.File.Create(r.Context(), actor.WorkspaceID, file.UploadInput{
			Name:           filepath.Base(part.Filename),
			MIMEType:       mimeType,
			SizeBytes:      part.Size,
			Body:           opened,
			OwnerType:      r.FormValue("owner_type"),
			OwnerID:        r.FormValue("owner_id"),
			UploadedByType: "user",
			UploadedByID:   actor.MemberID,
		})
		if err != nil {
			writeFileError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, fileJSON(*created))
	}
}

func handleDownloadFile(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		record, opened, err := deps.File.Open(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "File not found.")
			return
		}
		if !fileReadableByActor(actor, record) {
			httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "You do not have permission to download this file.")
			return
		}
		defer opened.Close()

		w.Header().Set("Content-Type", record.MIMEType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", record.SizeBytes))
		w.Header().Set("Content-Disposition", contentDisposition(record.Name))
		if _, err := io.Copy(w, opened); err != nil {
			// The response may already be partially written, so only log the
			// failure through the request logger rather than attempting another
			// JSON response.
			return
		}
	}
}

func fileReadableByActor(actor *authorization.Actor, record *file.Record) bool {
	if actor == nil || record == nil {
		return false
	}
	switch record.OwnerType {
	case "message", "conversation":
		return actor.Can(authorization.ConversationRead)
	case "ticket":
		return actor.Can(authorization.TicketManage)
	case "article":
		return actor.Can(authorization.KnowledgebaseManage)
	case "form_submission":
		return actor.Can(authorization.ConversationRead)
	case "workspace", "":
		return actor.Can(authorization.WorkspaceManage)
	default:
		return false
	}
}

func fileJSON(record file.Record) map[string]any {
	return map[string]any{
		"id": record.ID, "workspace_id": record.WorkspaceID, "name": record.Name,
		"mime_type": record.MIMEType, "size_bytes": record.SizeBytes,
		"checksum": file.ChecksumHex(record.Checksum),
		"url":      "/api/v1/files/" + record.ID, "created_at": record.CreatedAt,
	}
}

func contentDisposition(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." {
		name = "download"
	}
	// mime.FormatMediaType quotes and escapes the filename parameter, avoiding
	// response-header injection while retaining non-ASCII names where possible.
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

func writeFileError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, file.ErrTooLarge):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, file.ErrMimeNotAllowed):
		status = http.StatusUnsupportedMediaType
	case errors.Is(err, file.ErrInvalidOwner):
		status = http.StatusBadRequest
	}
	code := httpserver.CodeBadRequest
	if status >= 500 {
		code = httpserver.CodeInternalError
	}
	httpserver.WriteError(w, r, status, code, "The file could not be stored.")
}
