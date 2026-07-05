# ConverseAI Learning OS — Architecture

> Transform ConverseAI into a Learning Operating System that continuously optimizes DSA interview preparation through structured knowledge tracking, FSRS-based spaced repetition, and personalized study planning.

---

## Decisions

| # | Question | Decision |
|---|----------|----------|
| 1 | Problem Catalog | **Empty catalog, organic growth.** User adds problems. When a LeetCode link or number is provided, the system fetches the page via HTTP, then the LLM transforms the raw content into structured metadata. |
| 2 | Problem Logging | **Structured form + optional "AI Analyze" button.** Quick manual logging with AI enrichment trigger. |
| 3 | Dashboard Scope | **Remove Projects entirely.** Learning Dashboard replaces the Projects concept. |
| 4 | Spaced Repetition | **Use `go-fsrs/v4` package.** Each topic gets a native `fsrs.Card` for scheduling. |

---

## Data Models

### MongoDB Collections

```
topics              — DSA concept nodes (e.g., "Sliding Window")
patterns            — Algorithmic patterns (e.g., "Monotonic Queue")
problems            — LeetCode problems with linked topics/patterns
problem_attempts    — Individual solve sessions with confidence ratings
topic_mastery       — Per-topic mastery state with embedded fsrs.Card
review_sessions     — Daily session records
interview_questions — Auto-generated or manual interview questions
daily_plans         — Planner-generated daily missions
```

### Key Types

```go
// TopicMastery embeds fsrs.Card directly for native FSRS scheduling
type TopicMastery struct {
    ID                primitive.ObjectID `bson:"_id,omitempty"`
    UserID            primitive.ObjectID `bson:"user_id"`
    TopicID           primitive.ObjectID `bson:"topic_id"`
    TopicName         string             `bson:"topic_name"`
    ProblemsSolved    int                `bson:"problems_solved"`
    RepeatedMistakes  int                `bson:"repeated_mistakes"`
    InterviewReady    bool               `bson:"interview_ready"`
    FSRSCard          fsrs.Card          `bson:"fsrs_card"`
    UpdatedAt         time.Time          `bson:"updated_at"`
}

// Problem — auto-extracted from web + LLM or manually created
type Problem struct {
    ID              primitive.ObjectID   `bson:"_id,omitempty"`
    UserID          primitive.ObjectID   `bson:"user_id"`
    PlatformID      string               `bson:"platform_id"`       // "239"
    Platform        string               `bson:"platform"`          // "leetcode"
    Title           string               `bson:"title"`
    Difficulty      string               `bson:"difficulty"`
    URL             string               `bson:"url,omitempty"`
    PatternIDs      []primitive.ObjectID `bson:"pattern_ids"`
    TopicIDs        []primitive.ObjectID `bson:"topic_ids"`
    PatternNames    []string             `bson:"pattern_names"`     // Denormalized
    TopicNames      []string             `bson:"topic_names"`       // Denormalized
    Companies       []string             `bson:"companies,omitempty"`
    CreatedAt       time.Time            `bson:"created_at"`
}
```

---

## Problem Auto-Extraction Pipeline

When a user provides a LeetCode problem number or URL:

```
User Input ("239" or "https://leetcode.com/problems/sliding-window-maximum/")
    ↓
Build URL (if number: "https://leetcode.com/problems/{slug}/")
    ↓
HTTP Fetch (Go net/http — fetch the page content)
    ↓
Extract raw text/HTML content
    ↓
LLM Transform (Ollama — structured JSON extraction prompt)
    ↓
ProblemMetadata {
    platform_id, title, difficulty,
    patterns, topics, companies
}
    ↓
Auto-create Topics/Patterns (upsert by name)
    ↓
Persist Problem with linked IDs
```

This avoids hallucination: the LLM only transforms real data, it doesn't invent problem details.

---

## FSRS Integration

```go
// Initialize
params := fsrs.DefaultParam()
params.RequestRetention = 0.9
scheduler := fsrs.NewFSRS(params)

// Map confidence (1-5) → FSRS Rating
// 1 → Again, 2 → Hard, 3-4 → Good, 5 → Easy

// After each attempt: scheduler.Next(card, now, rating) → updated Card
// Retrievability: scheduler.GetRetrievability(card, now) → float64 (0-1)
// Due reviews: query topic_mastery WHERE fsrs_card.due <= NOW
```

---

## Architecture Layers

```
┌──────────────────────────────────────────────────┐
│                    Frontend                       │
│  Learn Dashboard  │  Problem Log  │  Chat         │
├──────────────────────────────────────────────────┤
│                   HTTP Handlers                   │
│  LearningHandler (17 endpoints)  │  ChatHandler   │
├──────────────────────────────────────────────────┤
│                   Services                        │
│  LearningService  │  LearningEngine  │ ChatSvc    │
│  KnowledgeExtractor (LLM)                         │
├──────────────────────────────────────────────────┤
│                  Repositories                     │
│  Topic │ Pattern │ Problem │ Attempt │ Mastery    │
│  ReviewSession │ InterviewQuestion │ DailyPlan    │
├──────────────────────────────────────────────────┤
│                 Infrastructure                    │
│  MongoDB  │  Ollama  │  MinIO  │  ChromaDB        │
└──────────────────────────────────────────────────┘
```

---

## API Surface

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/learn/dashboard` | Full dashboard payload |
| `GET` | `/api/learn/plan` | Today's daily plan |
| `GET` | `/api/learn/mastery` | All topic mastery states |
| `GET` | `/api/learn/stats` | Learning statistics |
| `POST` | `/api/learn/topics` | Create topic |
| `GET` | `/api/learn/topics` | List topics |
| `POST` | `/api/learn/patterns` | Create pattern |
| `GET` | `/api/learn/patterns` | List patterns |
| `POST` | `/api/learn/problems` | Log problem (manual) |
| `POST` | `/api/learn/problems/extract` | Auto-extract from URL/number |
| `GET` | `/api/learn/problems` | List problems |
| `GET` | `/api/learn/problems/get` | Get single problem |
| `POST` | `/api/learn/attempts` | Log attempt (core action) |
| `GET` | `/api/learn/attempts` | Attempt history |
| `POST` | `/api/learn/attempts/enrich` | AI analysis |
| `GET` | `/api/learn/reviews` | Due reviews |
| `GET` | `/api/learn/questions` | Interview questions |
