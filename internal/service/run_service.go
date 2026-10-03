package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"ai-chat/internal/agent"
	"ai-chat/internal/config"
	"ai-chat/internal/events"
	"ai-chat/internal/metrics"
	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/rag"
	"ai-chat/internal/repository"
	"ai-chat/internal/storage"
	"ai-chat/internal/util"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	maxPromptRunes   = 32000
	maxFilesPerTurn  = 10
	heartbeatEvery   = 10 * time.Second
	staleAfter       = 45 * time.Second
	resumeScanEvery  = 15 * time.Second
	maxRunAttempts   = 3
	partialFlushTick = time.Second
)

var (
	errShutdown  = errors.New("server shutting down")
	errCancelled = errors.New("cancelled by user")
	errLeaseLost = errors.New("run taken over by another instance")
	errGaveUp    = errors.New("the answer was interrupted too many times")

	// ErrRunInProgress is returned when the conversation is already answering.
	ErrRunInProgress = fmt.Errorf("%w: this conversation is still answering the previous message", ErrConflict)
)

var runTracer = otel.Tracer("ai-chat/run")

// Upload is a file sent with a message.
type Upload struct {
	Name string
	Data []byte
}

type StartRequest struct {
	UserID         string
	ConversationID string
	Model          string
	Prompt         string
	Files          []Upload
}

// StreamMessage is one message on a run's live stream. Answer text carries
// a sequence number so a client that (re)attaches mid-run can be sent a
// snapshot and then only the chunks after it.
type StreamMessage struct {
	Type      string `json:"type"` // delta | reset | thought | status | error | done
	Seq       int64  `json:"seq,omitempty"`
	Text      string `json:"text,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	Status    string `json:"status,omitempty"`
}

type RunService interface {
	// Start validates the request, stores the user's message and files, and
	// starts answering in the background.
	Start(ctx context.Context, req StartRequest) (*model.Run, error)
	// Attach streams a run's output from its current state until it ends.
	Attach(ctx context.Context, userID, runID string) (<-chan StreamMessage, error)
	Cancel(ctx context.Context, userID, runID string) error
	// Background resumes runs abandoned by a crashed or restarted instance
	// until ctx is done.
	Background(ctx context.Context)
	// Shutdown stops local runs and hands them back for resumption.
	Shutdown(ctx context.Context)
}

type runService struct {
	cfg      *config.Config
	chats    repository.ChatRepository
	runs     repository.RunRepository
	catalog  repository.SystemLLMRepository
	storage  storage.StorageService
	rag      rag.Service
	agent    *agent.Agent
	broker   events.Broker
	emitter  *Emitter
	instance string

	baseCtx    context.Context
	baseCancel context.CancelCauseFunc
	slots      chan struct{}
	wg         sync.WaitGroup

	mu    sync.Mutex
	local map[primitive.ObjectID]*activeRun
}

type activeRun struct {
	run    *model.Run
	ctx    context.Context
	cancel context.CancelCauseFunc // set before the run starts, never changed

	mu      sync.Mutex
	partial strings.Builder
	seq     int64
	dirty   bool
}

func NewRunService(cfg *config.Config, chats repository.ChatRepository, runs repository.RunRepository,
	catalog repository.SystemLLMRepository, store storage.StorageService, ragSvc rag.Service,
	ag *agent.Agent, broker events.Broker, emitter *Emitter) RunService {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	ctx, cancel := context.WithCancelCause(context.Background())
	return &runService{
		cfg: cfg, chats: chats, runs: runs, catalog: catalog, storage: store, rag: ragSvc,
		agent: ag, broker: broker, emitter: emitter,
		instance: hex.EncodeToString(b),
		baseCtx:  ctx, baseCancel: cancel,
		slots: make(chan struct{}, cfg.MaxConcurrentRuns),
		local: map[primitive.ObjectID]*activeRun{},
	}
}

// ---- starting a run ----

func (s *runService) Start(ctx context.Context, req StartRequest) (*model.Run, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, invalid("message must not be empty")
	}
	if utf8.RuneCountInString(prompt) > maxPromptRunes {
		return nil, invalid("message is too long (max %d characters)", maxPromptRunes)
	}
	modelName := strings.TrimSpace(req.Model)
	if modelName == "" {
		modelName = s.cfg.DefaultChatModel
	}
	if s.catalog.GetMetadata(modelName) == nil {
		return nil, invalid("unknown model %q", modelName)
	}
	if len(req.Files) > maxFilesPerTurn {
		return nil, invalid("at most %d files per message", maxFilesPerTurn)
	}
	for i := range req.Files {
		req.Files[i].Name = util.SanitizeFilename(req.Files[i].Name)
		ext := strings.ToLower(filepath.Ext(req.Files[i].Name))
		if !util.IsSupportedUpload(ext) {
			return nil, invalid("unsupported file type %q (images, PDFs and text files are supported)", ext)
		}
		if len(req.Files[i].Data) == 0 {
			return nil, invalid("file %q is empty", req.Files[i].Name)
		}
	}
	ids, err := parseIDs(req.ConversationID, req.UserID)
	if err != nil {
		return nil, err
	}
	convID, userID := ids[0], ids[1]
	if _, err := s.chats.GetOwnedConversation(ctx, convID, userID); err != nil {
		return nil, mapRepoErr(err)
	}

	// Everything is validated; from here on, state is written.
	runID := primitive.NewObjectID()
	if err := s.claimConversation(ctx, convID, userID, runID); err != nil {
		return nil, err
	}
	release := func() { _ = s.chats.ClearActiveRun(context.WithoutCancel(ctx), convID, runID) }

	var attachments []string
	for _, f := range req.Files {
		id, err := s.storage.Save(ctx, req.UserID, f.Name, f.Data)
		if err != nil {
			release()
			return nil, fmt.Errorf("store %s: %w", f.Name, err)
		}
		if !slices.Contains(attachments, id) {
			attachments = append(attachments, id)
		}
	}

	msgID, err := s.chats.AddMessage(ctx, convID, model.Message{
		Role: model.RoleUser, Content: prompt, ModelName: modelName, Attachments: attachments,
		RunID: runID, TokenCount: rag.EstimateTokens(prompt),
	})
	if err != nil {
		release()
		return nil, err
	}
	run := &model.Run{
		ID: runID, ConversationID: convID, UserID: userID, UserMessageID: msgID,
		Model: modelName, Status: model.RunQueued, Owner: s.instance,
	}
	if err := s.runs.Create(ctx, run); err != nil {
		release()
		return nil, err
	}
	s.emitter.Emit(ctx, convID, userID, runID, model.EventUserMessageReceived, map[string]any{
		"attachments": displayNames(attachments), "model": modelName, "message": "User message received.",
	})

	ar := &activeRun{run: run}
	s.launch(ar, false)
	return run, nil
}

// claimConversation marks the conversation as answering. A stale marker
// left by a run that already ended (or vanished) is cleared and retried.
func (s *runService) claimConversation(ctx context.Context, convID, userID, runID primitive.ObjectID) error {
	err := s.chats.ClaimActiveRun(ctx, convID, userID, runID)
	if !errors.Is(err, repository.ErrConflict) {
		return mapRepoErr(err)
	}
	conv, err := s.chats.GetOwnedConversation(ctx, convID, userID)
	if err != nil {
		return mapRepoErr(err)
	}
	if conv.ActiveRunID == nil {
		return s.chats.ClaimActiveRun(ctx, convID, userID, runID)
	}
	prev, err := s.runs.Get(ctx, *conv.ActiveRunID)
	if err == nil && !prev.Status.Terminal() {
		return ErrRunInProgress
	}
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	_ = s.chats.ClearActiveRun(ctx, convID, *conv.ActiveRunID)
	if err := s.chats.ClaimActiveRun(ctx, convID, userID, runID); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return ErrRunInProgress
		}
		return err
	}
	return nil
}

func (s *runService) launch(ar *activeRun, resumed bool) {
	ar.ctx, ar.cancel = context.WithCancelCause(s.baseCtx)
	s.mu.Lock()
	s.local[ar.run.ID] = ar
	s.mu.Unlock()
	s.wg.Add(1)
	go s.execute(ar, resumed)
}

// ---- executing ----

func (s *runService) execute(ar *activeRun, resumed bool) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.local, ar.run.ID)
		s.mu.Unlock()
	}()
	run := ar.run
	started := time.Now()

	defer ar.cancel(nil)
	ctx, cancelTimeout := context.WithTimeout(ar.ctx, s.cfg.RunTimeout)
	defer cancelTimeout()
	ctx, span := runTracer.Start(ctx, "run")
	defer span.End()
	span.SetAttributes(attribute.String("run.id", run.ID.Hex()), attribute.String("llm.model", run.Model), attribute.Bool("run.resumed", resumed))

	bgDone := make(chan struct{})
	defer close(bgDone)
	go s.heartbeat(ar, bgDone)
	go s.flushPartial(ar, bgDone)

	// Wait for an execution slot: this is the queue in front of the GPU.
	metrics.RunQueueDepth.Inc()
	select {
	case s.slots <- struct{}{}:
		metrics.RunQueueDepth.Dec()
	default:
		s.publish(ar, StreamMessage{Type: "status", Text: "Waiting for other answers to finish..."})
		select {
		case s.slots <- struct{}{}:
			metrics.RunQueueDepth.Dec()
		case <-ctx.Done():
			metrics.RunQueueDepth.Dec()
			s.finish(ctx, ar, nil, ctx.Err())
			return
		}
	}
	defer func() { <-s.slots }()
	metrics.RunStageDuration.WithLabelValues("queue").Observe(time.Since(started).Seconds())

	if err := s.runs.MarkRunning(ctx, run.ID, s.instance, agent.PromptVersion, nil); err != nil {
		s.finish(ctx, ar, nil, err)
		return
	}
	evType := model.EventRunStarted
	if resumed {
		evType = model.EventRunResumed
	}
	s.emit(ctx, run, evType, map[string]any{"model": run.Model, "attempt": run.Attempts + 1, "message": "Answering with " + run.Model})

	res, err := s.process(ctx, ar, resumed)
	s.finish(ctx, ar, res, err)
	metrics.RunStageDuration.WithLabelValues("total").Observe(time.Since(started).Seconds())
}

func (s *runService) heartbeat(ar *activeRun, done <-chan struct{}) {
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			hctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			cancelRequested, err := s.runs.Heartbeat(hctx, ar.run.ID, s.instance)
			cancel()
			switch {
			case errors.Is(err, repository.ErrLeaseLost):
				ar.cancel(errLeaseLost)
			case err != nil:
				slog.Warn("run heartbeat failed", "run", ar.run.ID.Hex(), "err", err)
			case cancelRequested:
				ar.cancel(errCancelled)
			}
		}
	}
}

func (s *runService) flushPartial(ar *activeRun, done <-chan struct{}) {
	t := time.NewTicker(partialFlushTick)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			s.savePartial(ar)
		}
	}
}

func (s *runService) savePartial(ar *activeRun) {
	ar.mu.Lock()
	if !ar.dirty {
		ar.mu.Unlock()
		return
	}
	text, seq := ar.partial.String(), ar.seq
	ar.dirty = false
	ar.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.runs.SavePartial(ctx, ar.run.ID, s.instance, text, seq); err != nil && !errors.Is(err, repository.ErrLeaseLost) {
		slog.Warn("save partial answer failed", "run", ar.run.ID.Hex(), "err", err)
	}
}

// publish sends a message to the run's live stream. Answer text (delta and
// reset) updates the snapshot and gets the next sequence number under the
// same lock, so a snapshot and the stream always line up.
func (s *runService) publish(ar *activeRun, msg StreamMessage) {
	ar.mu.Lock()
	defer ar.mu.Unlock()
	switch msg.Type {
	case "delta":
		ar.partial.WriteString(msg.Text)
		ar.seq++
		msg.Seq = ar.seq
		ar.dirty = true
	case "reset":
		ar.partial.Reset()
		ar.partial.WriteString(msg.Text)
		ar.seq++
		msg.Seq = ar.seq
		ar.dirty = true
	}
	data, _ := json.Marshal(msg)
	s.broker.Publish(context.Background(), events.RunTopic(ar.run.ID.Hex()), data)
}

func (s *runService) emit(ctx context.Context, run *model.Run, t model.EventType, payload map[string]any) {
	s.emitter.Emit(ctx, run.ConversationID, run.UserID, run.ID, t, payload)
}

// process builds the prompt and runs the agent.
func (s *runService) process(ctx context.Context, ar *activeRun, resumed bool) (*agent.Result, error) {
	run := ar.run
	conv, err := s.chats.GetConversation(ctx, run.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("load conversation: %w", err)
	}
	idx := slices.IndexFunc(conv.Messages, func(m model.Message) bool { return m.ID == run.UserMessageID })
	if idx < 0 {
		return nil, errors.New("the message being answered no longer exists")
	}
	userMsg := conv.Messages[idx]

	llm := s.catalog.GetMetadata(run.Model)
	if llm == nil {
		return nil, fmt.Errorf("model %s is no longer configured", run.Model)
	}

	// Only attachments up to this turn are visible to it.
	attachments := (&model.Conversation{Messages: conv.Messages[:idx+1]}).AttachmentIDs()
	textIDs, imageIDs := agent.SplitAttachments(attachments)
	_, turnImages := agent.SplitAttachments(userMsg.Attachments)
	turnText, _ := agent.SplitAttachments(userMsg.Attachments)

	// Route to the vision model when images arrive for a model without vision.
	if len(turnImages) > 0 && !llm.Has("vision") {
		if vm := s.catalog.GetMetadata(s.cfg.DefaultVisionModel); vm != nil && vm.Has("vision") {
			s.emit(ctx, run, model.EventModelRouted, map[string]any{
				"from": llm.ModelName, "to": vm.ModelName,
				"message": fmt.Sprintf("%s can't see images; answering with %s", llm.ModelName, vm.ModelName),
			})
			llm = vm
		}
	}

	stage := time.Now()
	fileContext, images := s.prepareFiles(ctx, run, conv.UserID.Hex(), textIDs, turnText, turnImages, llm.Has("vision"))
	metrics.RunStageDuration.WithLabelValues("files").Observe(time.Since(stage).Seconds())

	rc := &agent.RunContext{
		UserID: conv.UserID.Hex(), ConversationID: conv.ID.Hex(), ChatModel: llm.ModelName,
		TextFileIDs: textIDs, ImageFileIDs: imageIDs,
		Emit: func(t model.EventType, p map[string]any) { s.emit(ctx, run, t, p) },
	}

	retrieved := ""
	if len(textIDs) > 0 {
		stage = time.Now()
		evs, err := s.rag.Search(ctx, conv.UserID.Hex(), userMsg.Content, preRetrieveCount, textIDs)
		if err != nil {
			slog.Warn("document retrieval failed", "run", run.ID.Hex(), "err", err)
		}
		s.emit(ctx, run, model.EventRAGSearchFinished, map[string]any{
			"count": len(evs), "results": evs, "message": fmt.Sprintf("Retrieved %d passages from attached files", len(evs)),
		})
		if len(evs) > 0 {
			retrieved = agent.FormatDocumentEvidence(rc, evs)
		}
		metrics.RunStageDuration.WithLabelValues("retrieve").Observe(time.Since(stage).Seconds())
	}

	in := turnInput{
		conv: conv, userMsgIdx: idx, llm: llm, maxNumCtx: s.cfg.MaxNumCtx,
		useTools: llm.Has("tools"), fileContext: fileContext, retrieved: retrieved,
		images: images, attachments: attachments,
	}
	base, err := s.fitContext(ctx, run, &in)
	if err != nil {
		return nil, err
	}

	var state *agent.State
	if resumed && run.State != "" {
		state = &agent.State{}
		if err := json.Unmarshal([]byte(run.State), state); err != nil {
			return nil, fmt.Errorf("decode saved run state: %w", err)
		}
	}
	if resumed {
		// Text streamed during the interrupted round is regenerated.
		answer := ""
		if state != nil {
			answer = state.Answer
		}
		s.publish(ar, StreamMessage{Type: "reset", Text: answer})
	}

	return s.agent.Run(ctx, rc, agent.Request{
		Model: llm, Base: base, UseTools: in.useTools, State: state,
		OnDelta:   func(t string) { s.publish(ar, StreamMessage{Type: "delta", Text: t}) },
		OnThought: func(t string) { s.publish(ar, StreamMessage{Type: "thought", Text: t}) },
		OnRetry:   func(answer string) { s.publish(ar, StreamMessage{Type: "reset", Text: answer}) },
		OnRound: func(st *agent.State) error {
			raw, err := json.Marshal(st)
			if err != nil {
				return err
			}
			return s.runs.SaveState(ctx, run.ID, s.instance, string(raw), st.ModelsUsed)
		},
	})
}

// prepareFiles makes sure every text attachment of the conversation is
// indexed (once), and renders this turn's files for the prompt.
func (s *runService) prepareFiles(ctx context.Context, run *model.Run, userID string, textIDs, turnText, turnImages []string, vision bool) (string, []string) {
	cache := map[string]string{}
	load := func(id string) func() (string, error) {
		return func() (string, error) {
			if t, ok := cache[id]; ok {
				return t, nil
			}
			t, err := fileText(ctx, s.storage, id)
			if err == nil {
				cache[id] = t
			}
			return t, err
		}
	}
	for _, id := range textIDs {
		n, err := s.rag.IngestFile(ctx, userID, id, storage.DisplayName(id), load(id))
		if err != nil {
			slog.Warn("index file failed", "file", id, "err", err)
			s.emit(ctx, run, model.EventRAGIngested, map[string]any{"file": storage.DisplayName(id), "success": false, "message": "Could not index " + storage.DisplayName(id)})
			continue
		}
		if n > 0 {
			s.emit(ctx, run, model.EventRAGIngested, map[string]any{"file": storage.DisplayName(id), "chunks": n, "success": true,
				"message": fmt.Sprintf("Indexed %s (%d chunks)", storage.DisplayName(id), n)})
		}
	}

	var sb strings.Builder
	for _, id := range turnText {
		text, err := load(id)()
		if err != nil {
			slog.Warn("read attachment failed", "file", id, "err", err)
			continue
		}
		if strings.TrimSpace(text) == "" {
			fmt.Fprintf(&sb, "%s has no extractable text (it may be a scanned document).\n", storage.DisplayName(id))
			continue
		}
		sb.WriteString(inlineFile(id, text))
		s.emit(ctx, run, model.EventAttachmentResolved, map[string]any{"file": storage.DisplayName(id), "message": "Read " + storage.DisplayName(id)})
	}

	var images []string
	if vision {
		for _, id := range turnImages {
			data, err := s.storage.Get(ctx, id)
			if err != nil {
				slog.Warn("read image failed", "file", id, "err", err)
				continue
			}
			images = append(images, encodeImage(data))
			s.emit(ctx, run, model.EventAttachmentResolved, map[string]any{"file": storage.DisplayName(id), "message": "Attached image " + storage.DisplayName(id)})
		}
	}
	return sb.String(), images
}

// fitContext builds the messages and keeps them inside num_ctx: first by
// summarizing older history, then by trimming this turn's file text.
func (s *runService) fitContext(ctx context.Context, run *model.Run, in *turnInput) ([]ollama.ChatMessage, error) {
	budget := in.llm.NumCtx(s.cfg.MaxNumCtx) - answerReserve
	now := time.Now()
	msgs := buildMessages(*in, now)
	if float64(estimateMessages(msgs)) <= float64(budget)*summarizeAt {
		return msgs, nil
	}

	history := in.conv.Messages[:in.userMsgIdx]
	if text := transcript(history); text != "" {
		stage := time.Now()
		s.emit(ctx, run, model.EventSummarizationStarted, map[string]any{"message": "Summarizing earlier messages to fit the context window"})
		summary, tokens, err := s.agent.Deps().Summarize(ctx, in.llm.ModelName, in.conv.Summary, text)
		if err != nil {
			slog.Warn("summarization failed", "run", run.ID.Hex(), "err", err)
			s.emit(ctx, run, model.EventSummarizationFinished, map[string]any{"success": false, "message": "Summarization failed; dropping older messages instead"})
		} else {
			cutoff := history[len(history)-1].CreatedAt
			if err := s.chats.SetSummary(ctx, in.conv.ID, summary, tokens, cutoff); err != nil {
				return nil, err
			}
			s.emit(ctx, run, model.EventSummarizationFinished, map[string]any{"success": true, "summary_tokens": tokens, "message": "Earlier messages summarized"})
		}
		metrics.RunStageDuration.WithLabelValues("summarize").Observe(time.Since(stage).Seconds())
		// Either way, older messages leave the prompt: covered by the summary,
		// or dropped because the summary failed.
		conv := *in.conv
		conv.Messages = append([]model.Message(nil), conv.Messages...)
		if err == nil {
			conv.Summary = summary
		}
		for i := range conv.Messages[:in.userMsgIdx] {
			conv.Messages[i].IsSummarized = true
		}
		in.conv = &conv
		msgs = buildMessages(*in, now)
	}

	// Still too big: shrink this turn's inline file text and passages.
	for i := 0; i < 4 && estimateMessages(msgs) > budget; i++ {
		in.fileContext = util.TruncateRunes(in.fileContext, len(in.fileContext)/2)
		in.retrieved = util.TruncateRunes(in.retrieved, len(in.retrieved)*2/3)
		msgs = buildMessages(*in, now)
	}
	if estimateMessages(msgs) > budget {
		return nil, invalid("this message is too long for %s's context window", in.llm.ModelName)
	}
	return msgs, nil
}

// finish records the outcome of a run exactly once.
func (s *runService) finish(ctx context.Context, ar *activeRun, res *agent.Result, runErr error) {
	run := ar.run
	cause := context.Cause(ctx)
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()

	if runErr != nil && errors.Is(cause, errLeaseLost) {
		slog.Info("run taken over elsewhere", "run", run.ID.Hex())
		return
	}
	if runErr != nil && errors.Is(cause, errShutdown) {
		// Hand the run back; it resumes from its last saved round.
		s.savePartial(ar)
		if err := s.runs.Release(wctx, run.ID, s.instance); err != nil {
			slog.Warn("release run failed", "run", run.ID.Hex(), "err", err)
		}
		return
	}

	status := model.RunSucceeded
	errMsg := ""
	var content, reasoning string
	var promptTok, outTok, lastPrompt int
	modelName := run.Model
	switch {
	case runErr == nil:
		if used := res.State.ModelsUsed; len(used) > 0 {
			modelName = used[len(used)-1] // may differ after vision routing
		}
		content = res.Content + agent.CitedSourcesFooter(res.Content, res.Sources)
		reasoning = res.Reasoning
		promptTok, outTok = res.State.Prompt, res.State.Completion
		lastPrompt = res.LastPrompt + res.LastOutput
		if strings.TrimSpace(res.Content) == "" {
			content = "_(The model returned an empty answer.)_"
		}
	case errors.Is(cause, errCancelled) || errors.Is(runErr, errCancelled):
		status, errMsg = model.RunCancelled, "stopped"
	default:
		status = model.RunFailed
		errMsg = publicRunError(runErr, ctx)
		slog.Error("run failed", "run", run.ID.Hex(), "err", runErr)
	}
	if status != model.RunSucceeded {
		ar.mu.Lock()
		partial := ar.partial.String()
		ar.mu.Unlock()
		if strings.TrimSpace(partial) != "" {
			note := "_(stopped)_"
			if status == model.RunFailed {
				note = "_(answer interrupted: " + errMsg + ")_"
			}
			content = partial + "\n\n" + note
		}
	}

	msgID := primitive.NilObjectID
	if content != "" {
		id, err := s.chats.AddMessage(wctx, run.ConversationID, model.Message{
			Role: model.RoleAssistant, Content: content, Reasoning: reasoning, ModelName: modelName,
			RunID: run.ID, TokenCount: outTok,
		})
		if err != nil {
			slog.Error("save answer failed", "run", run.ID.Hex(), "err", err)
		} else {
			msgID = id
		}
	}
	if lastPrompt > 0 {
		_ = s.chats.SetTotalTokens(wctx, run.ConversationID, lastPrompt)
	}
	if err := s.runs.Finish(wctx, run.ID, s.instance, status, errMsg, promptTok, outTok); err != nil {
		slog.Warn("finish run failed", "run", run.ID.Hex(), "err", err)
	}
	if err := s.chats.ClearActiveRun(wctx, run.ConversationID, run.ID); err != nil {
		slog.Warn("clear active run failed", "run", run.ID.Hex(), "err", err)
	}
	metrics.RunsTotal.WithLabelValues(string(status)).Inc()

	payload := map[string]any{"status": status, "prompt_tokens": promptTok, "completion_tokens": outTok, "message": "Run " + string(status)}
	if errMsg != "" {
		payload["error"] = errMsg
	}
	s.emit(wctx, run, model.EventRunFinished, payload)
	if status == model.RunFailed {
		s.publish(ar, StreamMessage{Type: "error", Text: errMsg})
	}
	done := StreamMessage{Type: "done", Status: string(status)}
	if !msgID.IsZero() {
		done.MessageID = msgID.Hex()
	}
	s.publish(ar, done)
}

func publicRunError(err error, ctx context.Context) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "the answer took too long and was stopped"
	case errors.Is(err, ErrInvalidInput), errors.Is(err, errGaveUp):
		return err.Error()
	default:
		return "the model server failed to answer; please try again"
	}
}

// ---- cancel ----

func (s *runService) Cancel(ctx context.Context, userID, runID string) error {
	ids, err := parseIDs(runID, userID)
	if err != nil {
		return err
	}
	if err := s.runs.RequestCancel(ctx, ids[0], ids[1]); err != nil {
		return mapRepoErr(err)
	}
	// Stop immediately if it runs here; other instances notice on heartbeat.
	s.mu.Lock()
	ar := s.local[ids[0]]
	s.mu.Unlock()
	if ar != nil && ar.cancel != nil {
		ar.cancel(errCancelled)
	}
	return nil
}

// ---- attaching to a stream ----

func (s *runService) snapshot(ctx context.Context, runID primitive.ObjectID) (string, int64, model.RunStatus, error) {
	s.mu.Lock()
	ar := s.local[runID]
	s.mu.Unlock()
	if ar != nil {
		ar.mu.Lock()
		defer ar.mu.Unlock()
		return ar.partial.String(), ar.seq, model.RunRunning, nil
	}
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return "", 0, "", err
	}
	return run.Partial, run.Seq, run.Status, nil
}

func (s *runService) Attach(ctx context.Context, userID, runID string) (<-chan StreamMessage, error) {
	ids, err := parseIDs(runID, userID)
	if err != nil {
		return nil, err
	}
	run, err := s.runs.GetOwned(ctx, ids[0], ids[1])
	if err != nil {
		return nil, mapRepoErr(err)
	}
	out := make(chan StreamMessage, 64)
	if run.Status.Terminal() {
		out <- StreamMessage{Type: "done", Status: string(run.Status)}
		close(out)
		return out, nil
	}
	// Subscribe before taking the snapshot so nothing falls in between.
	sub, unsub := s.broker.Subscribe(ctx, events.RunTopic(runID))
	go func() {
		defer close(out)
		defer func() { unsub() }()
		send := func(m StreamMessage) bool {
			select {
			case out <- m:
				return true
			case <-ctx.Done():
				return false
			}
		}
		var lastSeq int64
		resync := func(minSeq int64) bool {
			// A remote run flushes its snapshot every second; wait for it to
			// cover what we missed.
			for i := 0; i < 10; i++ {
				text, seq, status, err := s.snapshot(ctx, run.ID)
				if err != nil {
					return false
				}
				if status.Terminal() {
					send(StreamMessage{Type: "done", Status: string(status)})
					return false
				}
				if seq >= minSeq || i == 9 {
					lastSeq = seq
					return send(StreamMessage{Type: "reset", Seq: seq, Text: text})
				}
				select {
				case <-time.After(300 * time.Millisecond):
				case <-ctx.Done():
					return false
				}
			}
			return false
		}
		if !resync(0) {
			return
		}
		poll := time.NewTicker(3 * time.Second)
		defer poll.Stop()
		for {
			select {
			case data, ok := <-sub:
				if !ok { // we lagged and were dropped: resubscribe and resync
					unsub()
					sub, unsub = s.broker.Subscribe(ctx, events.RunTopic(runID))
					if !resync(lastSeq + 1) {
						return
					}
					continue
				}
				var m StreamMessage
				if json.Unmarshal(data, &m) != nil {
					continue
				}
				if m.Type == "delta" || m.Type == "reset" {
					if m.Seq <= lastSeq {
						continue
					}
					if m.Type == "delta" && m.Seq > lastSeq+1 {
						if !resync(m.Seq - 1) {
							return
						}
						if m.Seq != lastSeq+1 {
							continue
						}
					}
					lastSeq = m.Seq
				}
				if !send(m) || m.Type == "done" {
					return
				}
			case <-poll.C:
				// Catch a run that ended while we weren't subscribed.
				if r, err := s.runs.Get(ctx, run.ID); err == nil && r.Status.Terminal() {
					send(StreamMessage{Type: "done", Status: string(r.Status)})
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// ---- resuming and shutdown ----

func (s *runService) Background(ctx context.Context) {
	t := time.NewTicker(resumeScanEvery)
	defer t.Stop()
	for {
		s.resumeStale(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *runService) resumeStale(ctx context.Context) {
	for {
		s.mu.Lock()
		busy := len(s.local)
		s.mu.Unlock()
		if busy >= 2*s.cfg.MaxConcurrentRuns || s.baseCtx.Err() != nil {
			return
		}
		run, err := s.runs.ClaimStale(ctx, s.instance, time.Now().Add(-staleAfter))
		if errors.Is(err, repository.ErrNotFound) {
			return
		}
		if err != nil {
			slog.Warn("claim stale run failed", "err", err)
			return
		}
		ar := &activeRun{run: run, seq: run.Seq}
		ar.partial.WriteString(run.Partial)
		if run.Attempts > maxRunAttempts {
			slog.Error("giving up on run", "run", run.ID.Hex(), "attempts", run.Attempts)
			s.finish(ctx, ar, nil, errGaveUp)
			continue
		}
		slog.Info("resuming run", "run", run.ID.Hex(), "attempt", run.Attempts)
		s.launch(ar, true)
	}
}

func (s *runService) Shutdown(ctx context.Context) {
	s.baseCancel(errShutdown)
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("runs did not stop before shutdown deadline")
	}
}
