# Chief of Staff AI — Personal Execution Operating System

Chief of Staff AI is a personal execution operating system that transforms long-term goals into daily actions, continuously tracks progress, identifies bottlenecks, reprioritizes work, and acts as an intelligent accountability partner.

Unlike standard productivity tools that merely store tasks, Chief of Staff AI manages **outcomes**.

---

## 🎯 Vision & Core Concepts

### 📊 Dashboard-Centric Flow
The core application acts as a personal execution dashboard. Users can easily view and manage their primary focus areas, schedules, action items, and context in a clean, unified space.

### 🎯 Goal Workspaces
Users can define structured **Goals** (e.g., "Become Senior Engineer at Google").
*   **Task Connections**: Each goal can be connected to specific external or internal tasks or task lists.
*   **Calendar Integration**: Each goal can be connected to external calendar services.
*   **Information Gathering**: Based on these connections, the application automatically pulls and synchronizes execution schedules, progress, and relevant constraints.

### 💬 Dual Chat Architecture (Strict Separation)
ConverseAI provides two separate chat environments to ensure context clarity and avoid cognitive pollution:
1.  **General Chat**: A general-purpose playground for general productivity planning, queries, and unstructured research.
2.  **Goal-Specific Chat**: Housed strictly inside each goal's workspace. Conversations here are fully grounded on the specific goal's tasks, progress, calendar events, and memory. General chats and goal-specific chats are kept completely isolated and never merged.

---

## 🎯 Vision Pillars

Most ambitious professionals do not fail because of a lack of knowledge. They fail because of:
*   **Poor Prioritization**: Inability to distinguish high-impact actions from noise.
*   **Competing Goals**: Attempting to execute too many objectives simultaneously.
*   **Lack of Accountability**: No objective entity tracking execution rates.
*   **Inconsistent Execution**: Difficulty in breaking down long-term goals into daily chunks.
*   **Context Switching & Forgetting**: Losing track of long-term constraints and decisions.

Chief of Staff AI answers a single question every day:
> **"What is the highest leverage action I should take right now to achieve my goal?"**

---

## ⛓️ Product Architecture Flow

```text
North Star Goal
        ↓
Goal Decomposition
        ↓
Quarterly Objectives
        ↓
Monthly Milestones
        ↓
Weekly Plans
        ↓
Daily Execution
        ↓
Feedback Loop (Weekly Review)
        ↓
Continuous Reprioritization & Reality Gap Adjustment
```

---

## 🚀 Core Features

### 1. Goal Decomposition Engine
Automatically breaks large, vague goals into multi-tiered executable milestones.
*   *Input Example*: `"Become Senior Engineer at Google"`
*   *Output Tree*:
    ```text
    Senior Engineer
    ├── Data Structures & Algorithms
    ├── System Design
    ├── Distributed Systems
    ├── Behavioral Interviews
    ├── Resume & Referrals
    └── OSS Contributions
    ```

### 2. Daily Command Center (Prioritized Agenda)
Surfaces only the highest leverage actions every morning based on impact, strategic alignment, and immediate priorities.
1.  Solve 2 Graph Problems (Impact: High, Alignment: DSA)
2.  Study Distributed Transactions (Impact: High, Alignment: System Design)
3.  Complete OSS Pull Request (Impact: Med, Alignment: Portfolio)
4.  Reach Out to 2 Referrals (Impact: Med, Alignment: Job Search)

### 3. Intelligent Prioritization Score (IPS)
Every task receives a dynamic score computed from:
$$\text{IPS} = f(\text{Impact}, \text{Urgency}, \text{Strategic Alignment}, \text{Dependency Risk}, \text{Effort})$$
The system continuously reorders outstanding tasks to maximize leverage.

### 4. Progress Intelligence
Measures actual readiness and competency rather than simple task checkmarks.
*   *Example (DSA)*: Arrays: 90% | Trees: 80% | Graphs: 50% | DP: 25%

### 5. Long-Term Memory System
Retains long-term contextual layers:
*   **Goals**: Target role, target timelines.
*   **Decisions**: "Selected Temporal for OSS contributions."
*   **Constraints**: "Preparing for sister's marriage," "Available time: 15 hrs/week."
*   **Lessons**: "Struggles with Dynamic Programming."

### 6. Killer Feature: Reality Gap Detection
Measures feasibility by comparing current readiness, target timeline, and available hours:
*   *Goal*: Google Senior Engineer in 6 Months.
*   *Analysis*: Current readiness is 40%. Projected readiness is 14 months at 10 hours/week.
*   *Recommendation*: To hit goal in 6 months, increase weekly study time to 25 hours.

### 7. Learning Engine
Integrates with custom topics (LeetCode, System Design, Behavioral Stories) using **FSRS-based spaced repetition** to optimize retention.

### 8. Weekly Executive Review
A weekly collaborative review session with the AI:
*   **Review**: Plan vs. Actual completions.
*   **Analysis**: Execution rate, goal alignment, major risks, bottlenecks.
*   **Outcome**: Automated course corrections and plan for next week.

---

## 🛠️ Technical Architecture

### Target System (Production Stack)
*   **Frontend**: Next.js, Tailwind CSS, ShadCN
*   **Backend**: FastAPI / Go
*   **Database**: PostgreSQL + pgvector
*   **Workflow Engine**: Temporal
*   **Agent Framework**: LangGraph
*   **LLM Providers**: OpenAI, Claude

### Current Pivot Implementation (V1 MVP Stack)
*   **Frontend**: React, Vite, TypeScript, Tailwind CSS / Custom CSS
*   **Backend**: Go (Golang)
*   **Database**: MongoDB
*   **Search**: ChromaDB / Local Vector Search
*   **Storage**: MinIO
*   **LLM Provider**: Ollama (Local LLM execution)

---

## 📈 MVP Scope Phases

*   **Phase 1 (Active)**: Goal Management, Task Generation, Competency Progress Tracking, Daily Planning, Weekly Reviews.
*   **Phase 2**: Learning Engine, FSRS Scheduler, Knowledge Tracking.
*   **Phase 3**: Opportunity Radar, Job Search Automation, OSS Discovery.
*   **Phase 4**: Multi-Agent Chief of Staff, Autonomous Planning, Full Career Operating System.
