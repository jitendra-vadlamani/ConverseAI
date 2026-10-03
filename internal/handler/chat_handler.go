package handler

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ai-chat/internal/middleware"
	"ai-chat/internal/service"
	"ai-chat/internal/storage"
	"ai-chat/internal/util"
)

type ChatHandler struct {
	chats          service.ChatService
	runs           service.RunService
	maxUploadBytes int64
}

func NewChatHandler(chats service.ChatService, runs service.RunService, maxUploadBytes int64) *ChatHandler {
	return &ChatHandler{chats: chats, runs: runs, maxUploadBytes: maxUploadBytes}
}

func (h *ChatHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	convs, err := h.chats.ListConversations(r.Context(), middleware.UserID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, convs)
}

func (h *ChatHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	conv, err := h.chats.GetConversation(r.Context(), middleware.UserID(r), r.URL.Query().Get("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

func (h *ChatHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	conv, err := h.chats.CreateConversation(r.Context(), middleware.UserID(r), req.Title)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, conv)
}

func (h *ChatHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	if err := h.chats.DeleteConversation(r.Context(), middleware.UserID(r), r.URL.Query().Get("id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ChatHandler) UpdateConversationTitle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.chats.UpdateConversationTitle(r.Context(), middleware.UserID(r), req.ID, req.Title); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ChatHandler) ListConversationFiles(w http.ResponseWriter, r *http.Request) {
	files, err := h.chats.ListConversationFiles(r.Context(), middleware.UserID(r), r.URL.Query().Get("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (h *ChatHandler) DeleteConversationFile(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if err := h.chats.DeleteConversationFile(r.Context(), middleware.UserID(r), q.Get("id"), q.Get("fileID")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DownloadFile streams a file through the API (MinIO is not reachable from
// browsers). Only images and PDFs are shown inline; everything else is a
// download, so an uploaded HTML or SVG file can never run script on this
// origin.
func (h *ChatHandler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	fileID := r.URL.Query().Get("fileID")
	obj, err := h.chats.OpenFile(r.Context(), middleware.UserID(r), fileID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer obj.Body.Close()

	name := storage.DisplayName(fileID)
	ext := strings.ToLower(filepath.Ext(name))
	disposition, ctype := "attachment", "application/octet-stream"
	hdr := w.Header()
	switch {
	case util.IsImage(ext):
		disposition, ctype = "inline", storage.ContentType(name)
		hdr.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	case ext == ".pdf":
		// Browser PDF viewers don't run page script on this origin, and a
		// strict CSP stops some of them from rendering at all.
		disposition, ctype = "inline", "application/pdf"
		hdr.Del("Content-Security-Policy")
	default:
		hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	}
	hdr.Set("Content-Type", ctype)
	hdr.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	hdr.Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	hdr.Set("Cache-Control", "private, max-age=300")
	_, _ = io.Copy(w, obj.Body)
}

func (h *ChatHandler) GetEvents(w http.ResponseWriter, r *http.Request) {
	evs, err := h.chats.GetEvents(r.Context(), middleware.UserID(r), r.URL.Query().Get("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, evs)
}

func (h *ChatHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.chats.ListModels())
}

// StreamEvents streams a conversation's System Logs as they happen.
func (h *ChatHandler) StreamEvents(w http.ResponseWriter, r *http.Request) {
	ch, cancel, err := h.chats.SubscribeEvents(r.Context(), middleware.UserID(r), r.URL.Query().Get("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer cancel()
	s := newSSE(w)
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return
			}
			if _, err := io.WriteString(w, "data: "+string(data)+"\n\n"); err != nil {
				return
			}
			if s.rc.Flush() != nil {
				return
			}
		case <-ping.C:
			if s.comment("ping") != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (h *ChatHandler) SubmitFeedback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConversationID string `json:"conversation_id"`
		MessageID      string `json:"message_id"`
		Rating         int    `json:"rating"`
		Correction     string `json:"correction"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.chats.SubmitFeedback(r.Context(), middleware.UserID(r), req.ConversationID, req.MessageID, req.Rating, req.Correction); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// StreamCompletion stores the user's message, starts the answer as a
// background run and streams it. If the client disconnects the run keeps
// going and the answer is saved; the client can re-attach with StreamRun.
func (h *ChatHandler) StreamCompletion(w http.ResponseWriter, r *http.Request) {
	req := service.StartRequest{UserID: middleware.UserID(r)}
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mt {
	case "multipart/form-data":
		r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes+(1<<20))
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			if _, ok := err.(*http.MaxBytesError); !ok {
				err = &service.ValidationError{Msg: "could not read the uploaded form"}
			}
			writeError(w, r, err)
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		req.ConversationID = r.FormValue("conversation_id")
		req.Model = r.FormValue("model_name")
		req.Prompt = r.FormValue("content")
		for _, fh := range r.MultipartForm.File["files"] {
			f, err := fh.Open()
			if err != nil {
				writeError(w, r, err)
				return
			}
			data, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				writeError(w, r, err)
				return
			}
			req.Files = append(req.Files, service.Upload{Name: fh.Filename, Data: data})
		}
	default:
		var body struct {
			ConversationID string `json:"conversation_id"`
			ModelName      string `json:"model_name"`
			Content        string `json:"content"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			writeError(w, r, err)
			return
		}
		req.ConversationID, req.Model, req.Prompt = body.ConversationID, body.ModelName, body.Content
	}

	run, err := h.runs.Start(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	h.streamRun(w, r, run.ID.Hex(), run.ConversationID.Hex())
}

// StreamRun re-attaches to a running answer, e.g. after a page reload.
func (h *ChatHandler) StreamRun(w http.ResponseWriter, r *http.Request) {
	h.streamRun(w, r, r.URL.Query().Get("id"), "")
}

func (h *ChatHandler) streamRun(w http.ResponseWriter, r *http.Request, runID, convID string) {
	msgs, err := h.runs.Attach(r.Context(), middleware.UserID(r), runID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	s := newSSE(w)
	if s.event("run", map[string]string{"run_id": runID, "conversation_id": convID}) != nil {
		return
	}
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case m, ok := <-msgs:
			if !ok {
				return
			}
			if s.event(m.Type, m) != nil {
				return
			}
		case <-ping.C:
			if s.comment("ping") != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (h *ChatHandler) CancelRun(w http.ResponseWriter, r *http.Request) {
	if err := h.runs.Cancel(r.Context(), middleware.UserID(r), r.URL.Query().Get("id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
