package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"ai-chat/internal/events"
	"ai-chat/internal/model"
	"ai-chat/internal/rag"
	"ai-chat/internal/repository"
	"ai-chat/internal/storage"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const maxTitleLen = 120

// Emitter records System Logs events (encrypted at rest) and publishes them
// to live subscribers.
type Emitter struct {
	Events repository.EventRepository
	Broker events.Broker
}

func (e *Emitter) Emit(ctx context.Context, convID, userID, runID primitive.ObjectID, t model.EventType, payload map[string]any) {
	ev := model.ConversationEvent{
		ConversationID: convID, UserID: userID, RunID: runID,
		Type: t, Payload: payload, Timestamp: time.Now(),
	}
	// Events are written even if the request that caused them is gone.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := e.Events.StoreEvent(sctx, &ev); err != nil {
		slog.Warn("store event failed", "type", t, "err", err)
	}
	if data, err := json.Marshal(ev); err == nil {
		e.Broker.Publish(sctx, events.ConversationTopic(convID.Hex()), data)
	}
}

type ChatService interface {
	CreateConversation(ctx context.Context, userID, title string) (*model.Conversation, error)
	// GetConversation returns ErrNotFound unless the user owns it.
	GetConversation(ctx context.Context, userID, id string) (*model.Conversation, error)
	ListConversations(ctx context.Context, userID string) ([]model.ConversationSummary, error)
	UpdateConversationTitle(ctx context.Context, userID, id, title string) error
	DeleteConversation(ctx context.Context, userID, id string) error
	GetEvents(ctx context.Context, userID, id string) ([]model.ConversationEvent, error)
	// SubscribeEvents checks ownership, then subscribes to live events.
	SubscribeEvents(ctx context.Context, userID, id string) (<-chan []byte, func(), error)
	ListModels() []model.LLMConfig
	ListConversationFiles(ctx context.Context, userID, id string) ([]string, error)
	DeleteConversationFile(ctx context.Context, userID, id, fileID string) error
	// OpenFile streams a file the user owns.
	OpenFile(ctx context.Context, userID, fileID string) (*storage.Object, error)
	SubmitFeedback(ctx context.Context, userID, convID, messageID string, rating int, correction string) error
	// DeleteAccount removes the user and everything they stored: chats,
	// events, runs, feedback, files and vectors.
	DeleteAccount(ctx context.Context, userID string) error
}

type chatService struct {
	repo     repository.ChatRepository
	users    repository.UserRepository
	runs     repository.RunRepository
	feedback repository.FeedbackRepository
	events   repository.EventRepository
	catalog  repository.SystemLLMRepository
	storage  storage.StorageService
	rag      rag.Service
	broker   events.Broker
}

func NewChatService(repo repository.ChatRepository, users repository.UserRepository, runs repository.RunRepository,
	feedback repository.FeedbackRepository, eventRepo repository.EventRepository, catalog repository.SystemLLMRepository,
	store storage.StorageService, ragSvc rag.Service, broker events.Broker) ChatService {
	return &chatService{
		repo: repo, users: users, runs: runs, feedback: feedback, events: eventRepo, catalog: catalog,
		storage: store, rag: ragSvc, broker: broker,
	}
}

func parseIDs(ids ...string) ([]primitive.ObjectID, error) {
	out := make([]primitive.ObjectID, len(ids))
	for i, id := range ids {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return nil, ErrNotFound
		}
		out[i] = oid
	}
	return out, nil
}

func mapRepoErr(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, repository.ErrConflict):
		return ErrConflict
	}
	return err
}

func cleanTitle(title string) (string, error) {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return "", invalid("title must not be empty")
	}
	if utf8.RuneCountInString(title) > maxTitleLen {
		title = string([]rune(title)[:maxTitleLen])
	}
	return title, nil
}

func (s *chatService) owned(ctx context.Context, userID, id string) (*model.Conversation, error) {
	ids, err := parseIDs(id, userID)
	if err != nil {
		return nil, err
	}
	conv, err := s.repo.GetOwnedConversation(ctx, ids[0], ids[1])
	return conv, mapRepoErr(err)
}

func (s *chatService) CreateConversation(ctx context.Context, userID, title string) (*model.Conversation, error) {
	ids, err := parseIDs(userID)
	if err != nil {
		return nil, err
	}
	if title, err = cleanTitle(title); err != nil {
		title = "New chat"
	}
	return s.repo.CreateConversation(ctx, ids[0], title)
}

func (s *chatService) GetConversation(ctx context.Context, userID, id string) (*model.Conversation, error) {
	return s.owned(ctx, userID, id)
}

func (s *chatService) ListConversations(ctx context.Context, userID string) ([]model.ConversationSummary, error) {
	ids, err := parseIDs(userID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListConversations(ctx, ids[0])
}

func (s *chatService) UpdateConversationTitle(ctx context.Context, userID, id, title string) error {
	ids, err := parseIDs(id, userID)
	if err != nil {
		return err
	}
	if title, err = cleanTitle(title); err != nil {
		return err
	}
	return mapRepoErr(s.repo.UpdateTitle(ctx, ids[0], ids[1], title))
}

func (s *chatService) DeleteConversation(ctx context.Context, userID, id string) error {
	conv, err := s.owned(ctx, userID, id)
	if err != nil {
		return err
	}
	if conv.ActiveRunID != nil {
		return fmt.Errorf("%w: stop the running answer before deleting this conversation", ErrConflict)
	}
	if err := s.repo.DeleteConversation(ctx, conv.ID); err != nil {
		return err
	}
	// Files are shared across a user's conversations by content; purge only
	// those no other conversation still references.
	for _, fileID := range conv.AttachmentIDs() {
		s.purgeFileIfUnused(ctx, conv.UserID, fileID)
	}
	if err := s.events.DeleteEventsByConversationID(ctx, conv.ID); err != nil {
		slog.Warn("delete conversation events", "conversation", conv.ID.Hex(), "err", err)
	}
	if err := s.feedback.DeleteByConversation(ctx, conv.ID); err != nil {
		slog.Warn("delete conversation feedback", "conversation", conv.ID.Hex(), "err", err)
	}
	return nil
}

func (s *chatService) purgeFileIfUnused(ctx context.Context, userID primitive.ObjectID, fileID string) {
	count, err := s.repo.CountFileReferences(ctx, userID, fileID)
	if err != nil || count > 0 {
		return
	}
	if err := s.storage.Delete(ctx, fileID); err != nil {
		slog.Warn("delete file", "file", fileID, "err", err)
	}
	if err := s.rag.DeleteFile(ctx, userID.Hex(), fileID); err != nil {
		slog.Warn("delete file vectors", "file", fileID, "err", err)
	}
}

func (s *chatService) GetEvents(ctx context.Context, userID, id string) ([]model.ConversationEvent, error) {
	conv, err := s.owned(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return s.events.GetEventsByConversationID(ctx, conv.ID)
}

func (s *chatService) SubscribeEvents(ctx context.Context, userID, id string) (<-chan []byte, func(), error) {
	conv, err := s.owned(ctx, userID, id)
	if err != nil {
		return nil, nil, err
	}
	ch, cancel := s.broker.Subscribe(ctx, events.ConversationTopic(conv.ID.Hex()))
	return ch, cancel, nil
}

func (s *chatService) ListModels() []model.LLMConfig { return s.catalog.GetAllSystemModels() }

func (s *chatService) ListConversationFiles(ctx context.Context, userID, id string) ([]string, error) {
	conv, err := s.owned(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return conv.AttachmentIDs(), nil
}

func (s *chatService) DeleteConversationFile(ctx context.Context, userID, id, fileID string) error {
	conv, err := s.owned(ctx, userID, id)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(fileID, storage.UserPrefix(userID)) {
		return ErrNotFound
	}
	if err := s.repo.RemoveFileFromConversation(ctx, conv.ID, fileID); err != nil {
		return err
	}
	s.purgeFileIfUnused(ctx, conv.UserID, fileID)
	return nil
}

func (s *chatService) OpenFile(ctx context.Context, userID, fileID string) (*storage.Object, error) {
	// Object keys are namespaced per user, so the prefix is the ownership check.
	if !strings.HasPrefix(fileID, storage.UserPrefix(userID)) || strings.Contains(fileID, "..") {
		return nil, ErrNotFound
	}
	obj, err := s.storage.Open(ctx, fileID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrNotFound
	}
	return obj, err
}

func (s *chatService) SubmitFeedback(ctx context.Context, userID, convID, messageID string, rating int, correction string) error {
	if rating != 1 && rating != -1 {
		return invalid("rating must be 1 or -1")
	}
	if utf8.RuneCountInString(correction) > 4000 {
		return invalid("correction must be at most 4000 characters")
	}
	conv, err := s.owned(ctx, userID, convID)
	if err != nil {
		return err
	}
	msgID, err := primitive.ObjectIDFromHex(messageID)
	if err != nil {
		return ErrNotFound
	}
	for _, m := range conv.Messages {
		if m.ID == msgID && m.Role == model.RoleAssistant {
			return s.feedback.Upsert(ctx, &model.Feedback{
				UserID: conv.UserID, ConversationID: conv.ID, MessageID: msgID, RunID: m.RunID,
				Rating: rating, Correction: strings.TrimSpace(correction),
			})
		}
	}
	return ErrNotFound
}

func (s *chatService) DeleteAccount(ctx context.Context, userID string) error {
	ids, err := parseIDs(userID)
	if err != nil {
		return err
	}
	uid := ids[0]
	// Delete external data first: if this fails half-way the account still
	// exists and the user can retry.
	if err := s.storage.DeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("delete files: %w", err)
	}
	if err := s.rag.DeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("delete vectors: %w", err)
	}
	for name, del := range map[string]func(context.Context, primitive.ObjectID) error{
		"conversations": s.repo.DeleteByUser,
		"events":        s.events.DeleteByUser,
		"runs":          s.runs.DeleteByUser,
		"feedback":      s.feedback.DeleteByUser,
	} {
		if err := del(ctx, uid); err != nil {
			return fmt.Errorf("delete %s: %w", name, err)
		}
	}
	return s.users.Delete(ctx, uid)
}
