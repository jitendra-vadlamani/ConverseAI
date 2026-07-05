# ConverseAI: Knowledge-Graph Topic Explorer

ConverseAI is a personal execution tool centered on a topic knowledge graph (DSA, System Design, etc.), helping ambitious professionals decompose large study domains into nested sub-topics, map prerequisites, track node-level mastery, and study with an on-demand AI tutor.

---

## 🎯 Core Design & Concepts

### 🌳 Sidebar Tree (Hierarchy)
Navigates the main topic hierarchy built via parent-child `part_of` relations. Easily expand or collapse domains (e.g., *Computer Science* &rarr; *Data Structures & Algorithms* &rarr; *Arrays & Strings*).

### 🕸️ Interactive Local Graph View
Visualizes local connections (1-2 hops out from the selected node) dynamically using a clean, native SVG interface. Displays:
*   **`part_of`**: Hierarchy links (parent and sub-topics).
*   **`prerequisite_of`**: Prerequisite requirements.
*   **`related_to`**: Side relations.
Clicking any node in the local graph selects it as the active node.

### 🔒 Prerequisite Locks
Ensures structured, step-by-step learning. A topic is **locked** if any of its defined prerequisite topics have a mastery score below **70%**. Locked topics hide practice artifacts until prerequisites are met.

### 💬 Scoped AI Tutor
Each topic node features a dedicated AI tutoring chat. The conversation is fully grounded on the specific topic node, its description, your current mastery level, and custom study notes. 

### 📅 Daily Prioritized Agenda
Computes your daily study recommendations using a custom **Intelligent Prioritization Score (IPS)**:
$$\text{IPS} = (100 - \text{MasteryScore}) + \text{DaysSinceLastReviewed}$$
Uncompleted, unlocked leaf topics with low mastery scores or old review dates are highlighted to maximize learning leverage.

### 📊 Weekly Graph Reviews
Runs a strategic evaluation of your knowledge graph. Highlights:
*   Your overall mastery status (nodes $\ge$ 70% score).
*   Prerequisite bottlenecks blocking subsequent learning paths.
*   Priority recommendations on what to study next week to unlock the most nodes.

---

## ⛓️ Technical Architecture

Unlike the previous multi-user dashboard MVP, the refactored architecture is simplified to a single-user system:

*   **Frontend**: React, Vite, TypeScript, Custom Vanilla CSS
*   **Backend**: Go (Golang)
*   **Database**: PostgreSQL (handling adjacency-list schema for recursive graph trees)
*   **LLM Provider**: Ollama (supports local or remote models like `gemma4:e4b` or `gemma4-cos` with custom context windows)
*   **Infrastructure**: Dropped MongoDB, MinIO storage, ChromaDB vector databases, and heavy orchestrators for clean execution.

---

## 🏃‍♂️ Quickstart & Local Development

### Prerequisites
- [Docker & Docker Compose](https://www.docker.com/)
- [Go 1.22+](https://go.dev/)
- [Node.js & npm](https://nodejs.org/)

### 1. Start PostgreSQL Database
```bash
# Starts the converseai-pg container and applies migration tables automatically
make infra-up
```

### 2. Seed Database Graph
```bash
# Connects to PostgreSQL and loads the hand-curated baseline graph (DSA + System Design)
docker exec -i converseai-pg psql -U postgres -d converseai < scripts/seed.sql
```

### 3. Run Backend Server
Ensure your `.env` contains your Ollama server endpoint. If you have Ollama running at a remote IP (e.g., `http://192.168.10.106:11434`), configure it in your `.env` or as environment variables:
```bash
make dev-server
```

### 4. Run Frontend Client
In a new terminal, launch the Vite development server:
```bash
make dev-client
```

---

## 🛠️ Makefile Commands

- `make build-all`: Builds the production bundle of the React client and embeds it in the compiled Go server binary (`ai-chat-app`).
- `make infra-up`: Starts the PostgreSQL container.
- `make infra-down`: Stops and cleans up the PostgreSQL container.
- `make clean`: Removes client production dist folders and built Go binaries.
