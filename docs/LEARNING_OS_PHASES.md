# Learning OS — Implementation Phases

Each phase is self-contained and delivers a working increment. Build, test, and verify each phase before moving to the next.

---

## Phase 1: Foundation — Data Models + Repositories + Projects Cleanup

**Goal:** Establish the data layer and remove dead code. After this phase, the codebase is clean and the Learning Graph schema exists in MongoDB.

### 1.1 Remove Projects System

| Action | File | What |
|--------|------|------|
| DELETE | `internal/model/cos.go` | Remove `Project`, `ProjectTask`, `Competency`, `MemoryItem` |
| DELETE | `internal/repository/cos_repository.go` | Remove `ProjectRepository` + `MongoProjectRepository` |
| DELETE | `internal/service/cos_service.go` | Remove `CosService` (goal decomposition, reality gap, weekly review) |
| DELETE | `internal/handler/cos_handler.go` | Remove `ProjectHandler` + `/api/projects/*` routes |
| MODIFY | `internal/model/chat.go` | Remove `ProjectID` field from `Conversation` |
| MODIFY | `internal/service/chat_service.go` | Remove `projectRepo` field, remove project-aware system prompt in `prepareMessages()` |
| MODIFY | `internal/app/app.go` | Remove `projectRepo`, `cosService`, `projectHandler` instantiation; remove from `ChatService` constructor |

### 1.2 Add Learning Domain Models

| Action | File | What |
|--------|------|------|
| NEW | `internal/model/learning.go` | All 8 domain types: `Topic`, `Pattern`, `Problem`, `ProblemAttempt`, `TopicMastery` (with `fsrs.Card`), `ReviewSession`, `InterviewQuestion`, `DailyPlan` |

### 1.3 Add Learning Repositories

| Action | File | What |
|--------|------|------|
| NEW | `internal/repository/learning_repository.go` | 8 repository interfaces + MongoDB implementations |
| MODIFY | `internal/app/app.go` | Instantiate learning repos, create MongoDB indexes |

### 1.4 Add go-fsrs Dependency

```bash
cd /Users/jitendravadlamani/workspace/rnd/ConverseAI
go get github.com/open-spaced-repetition/go-fsrs/v4@latest
```

### Verification

```bash
go build ./...   # Must compile cleanly
```

- Confirm `/api/projects/*` returns 404
- Confirm chat still works without project system prompts

---

## Phase 2: Learning Engine — FSRS Scheduling + Mastery + Planning

**Goal:** The deterministic core brain. After this phase, the system can schedule reviews, calculate mastery, and generate daily plans — all without LLM.

### 2.1 Learning Engine

| Action | File | What |
|--------|------|------|
| NEW | `internal/service/learning_engine.go` | `LearningEngine` interface + implementation |

Features:
- `ReviewTopic()` — calls `fsrs.Next(card, now, rating)`, updates `TopicMastery`
- `GetRetrievability()` — calls `fsrs.GetRetrievability(card, now)`
- `GetDueReviews()` — queries mastery where `fsrs_card.due <= now`
- `RecalculateMastery()` — aggregates attempt data, sets `interview_ready`
- `GetMasteryDashboard()` — returns all mastery states with retrievability
- `GenerateDailyPlan()` — deterministic plan from due reviews + weakest topics
- `ProcessAttempt()` — maps confidence→rating, calls ReviewTopic
- `GetLearningStats()` — aggregate stats (total solved, retention, streak)

**Confidence → FSRS Rating mapping:**
```
1 → Again (complete failure)
2 → Hard  (significant difficulty)
3 → Good  (correct with effort)
4 → Good  (correct, comfortable)
5 → Easy  (effortless recall)
```

### Verification

Write a small test in `scratch/` that:
1. Creates a `TopicMastery` with `fsrs.NewCard()`
2. Calls `ReviewTopic` with rating=Good
3. Verifies `fsrs_card.due` moved forward
4. Calls `GetRetrievability` and verifies it returns ~0.9

```bash
go build ./...
```

---

## Phase 3: AI Layer — Knowledge Extractor + Problem Auto-Extraction

**Goal:** LLM-powered analysis. After this phase, problems can be auto-extracted from LeetCode URLs and solutions can be analyzed for patterns/mistakes.

### 3.1 Knowledge Extractor

| Action | File | What |
|--------|------|------|
| NEW | `internal/service/knowledge_extractor.go` | `KnowledgeExtractor` interface + implementation |

Features:
- `ExtractProblemMetadata(input string)` — the web-fetch + LLM pipeline:
  1. Parse input: detect if number (e.g., "239") or URL
  2. If number: build URL `https://leetcode.com/problems/...` (use LLM to get slug from number, or try common patterns)
  3. **HTTP Fetch**: Use `net/http` to GET the page content
  4. **Extract text**: Strip HTML to get problem description, title, difficulty
  5. **LLM Transform**: Send raw text to Ollama with structured JSON prompt → get `ProblemMetadata`
  6. Return structured result

- `AnalyzeSolution(problem, attempt)` — sends solution + notes to LLM, extracts:
  - Patterns used
  - Mistakes made
  - Interview questions
  - Extension problems
  - Suggested confidence

- `GenerateInterviewQuestions(topic, count)` — LLM generates questions for a topic

### Verification

```bash
go build ./...
```

Manual test: call `ExtractProblemMetadata("https://leetcode.com/problems/two-sum/")` and verify structured output.

---

## Phase 4: Learning Service + HTTP API

**Goal:** Wire everything together with a service orchestration layer and REST endpoints. After this phase, the full backend API is live.

### 4.1 Learning Service

| Action | File | What |
|--------|------|------|
| NEW | `internal/service/learning_service.go` | `LearningService` interface + implementation |

Orchestrates:
- `LogProblem()` — manual problem creation
- `ExtractAndLogProblem(input)` — calls extractor → auto-creates topics/patterns → persists problem
- `LogAttempt()` — validate → persist → map confidence→rating → engine.ReviewTopic → engine.RecalculateMastery
- `EnrichAttempt(attemptID)` — calls extractor.AnalyzeSolution → updates attempt with AI fields
- `GetDailyPlan()` — delegates to engine
- `GetMasteryOverview()` — delegates to engine
- CRUD for topics, patterns, problems, interview questions

### 4.2 HTTP Handler

| Action | File | What |
|--------|------|------|
| NEW | `internal/handler/learning_handler.go` | `LearningHandler` + `RegisterRoutes()` with 17 endpoints |

### 4.3 Wiring

| Action | File | What |
|--------|------|------|
| MODIFY | `internal/app/app.go` | Instantiate `LearningEngine`, `KnowledgeExtractor`, `LearningService`, `LearningHandler`; register routes |

### Verification

```bash
go build ./...
```

API smoke tests with curl:
```bash
# Create a topic
curl -X POST http://localhost:8080/api/learn/topics \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"name":"Sliding Window","category":"DSA"}'

# Extract a problem from LeetCode
curl -X POST http://localhost:8080/api/learn/problems/extract \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"input":"https://leetcode.com/problems/two-sum/"}'

# Log an attempt
curl -X POST http://localhost:8080/api/learn/attempts \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"problem_id":"...","confidence":4,"solve_time_min":25,"notes":"Used hash map"}'

# Get dashboard
curl http://localhost:8080/api/learn/dashboard \
  -H "Authorization: Bearer $TOKEN"
```

---

## Phase 5: Frontend — Learning Dashboard + Problem Log

**Goal:** The user-facing UI. After this phase, the full Learning OS is usable end-to-end.

### 5.1 Frontend API Client

| Action | File | What |
|--------|------|------|
| NEW | `client/src/api/learning.ts` | TypeScript API client for all `/api/learn/*` endpoints |

### 5.2 Learning Dashboard Page

| Action | File | What |
|--------|------|------|
| NEW | `client/src/pages/Learn.tsx` | Today's Mission dashboard |

Sections:
1. **Daily Mission Card** — review count, new problems, estimated time
2. **Mastery Heat Map** — color-coded grid (retrievability per topic)
3. **Overall Retention** — percentage with trend
4. **Due Reviews** — clickable problem list
5. **Weakest Topics** — bottom 3 by stability
6. **Recent Activity** — last 5 attempts
7. **Interview Questions** — random questions with reveal
8. **Quick Stats** — total solved, streak, interview-ready count

Design: Dark theme, glassmorphism, purple/cyan gradients, micro-animations.

### 5.3 Problem Log Page

| Action | File | What |
|--------|------|------|
| NEW | `client/src/pages/ProblemLog.tsx` | Problem logging + AI enrichment |

Features:
1. **Quick Add**: Paste URL or type number → "Extract" button → auto-fill
2. **Attempt Form**: Timer, notes, patterns, mistakes, confidence stars
3. **"AI Analyze" Button**: Enriches with patterns/mistakes/questions
4. **Submit**: Logs attempt, updates mastery

### 5.4 Route + Navigation Updates

| Action | File | What |
|--------|------|------|
| DELETE | `client/src/pages/ProjectsList.tsx` | Remove projects page |
| DELETE | `client/src/pages/Dashboard.tsx` | Remove old dashboard |
| MODIFY | `client/src/App.tsx` | Remove `/projects*` routes, add `/learn` + `/learn/log` |
| MODIFY | `client/src/layouts/MainLayout.tsx` | Remove "Projects" nav, add "Learn" + "Log Problem" |

### Verification

```bash
cd client && npm run build   # Must compile cleanly
```

Manual verification:
1. Navigate to `/learn` — dashboard renders with data
2. Navigate to `/learn/log` — form works, "Extract" button fetches problem
3. Submit an attempt — dashboard updates
4. Chat at `/` still works normally

---

## Phase Summary

| Phase | Deliverable | Dependencies | Estimated Files |
|-------|------------|--------------|-----------------|
| **1** | Clean codebase + data models + repos | None | ~10 files (6 delete, 3 modify, 2 new) |
| **2** | FSRS scheduling + mastery engine | Phase 1 | 1 new file |
| **3** | LLM extraction + web fetch | Phase 1 | 1 new file |
| **4** | REST API + service wiring | Phases 2 + 3 | 3 new files, 1 modify |
| **5** | Frontend dashboard + problem log | Phase 4 | 4 new files, 4 modify/delete |

Each phase builds on the previous. No phase requires the next to function. You can ship Phase 4 (backend-only) and use curl while Phase 5 is built.
