# High-Fidelity Agentic Search & Reasoning Engine

The ConverseAI Reasoning Engine is a multi-stage, iterative agent designed to solve complex, multi-part, and factual queries with high transparency and groundedness.

## Core Architecture

The engine follows an iterative loop: **Plan → Search → Extract → Evaluate → Synthesize.**

### 1. Step 0: Query Decomposition
Before any search occurs, the **Planner** analyzes the user's intent. If the query is complex (comparison, multi-hop, or open-ended), it is decomposed into independent searchable sub-tasks.
- *Example*: "Kafka vs Pulsar" is split into "Kafka architecture/performance" and "Pulsar architecture/performance".

### 2. Parallel Extraction & Scored RAG
For each sub-task:
1.  **Web Search**: DDG and Wikipedia are queried for relevant URLs.
2.  **Parallel Scrape**: Content is extracted from multiple pages simultaneously using the scraping service.
3.  **Local Indexing**: Extracted content is temporarily indexed in ChromaDB.
4.  **Scored Retrieval**: Chunks are retrieved and ranked based on a composite score:
    -   **Relevance (60%)**: Semantic similarity to the sub-query.
    -   **Authority (20%)**: Domain reputation (e.g., GitHub, StackOverflow, Official Docs).
    -   **Freshness (20%)**: Content age detected via regex-based date parsing.

### 3. Sufficiency Evaluation
After a search cycle, the system performs a **Sufficiency Check**:
-   The AI compares the found evidence against the original query aspects.
-   It identifies **Covered** vs **Missing** information.
-   If confidence is below a threshold, the loop repeats with reformulated queries to find the missing data.

### 4. Precision & Fact-Checking
To ensure the highest accuracy, the engine performs automated verification:
-   **Semantic Clustering**: Similar claims from different sources are grouped.
-   **Contradiction Detection**: The LLM analyzes clusters for disagreements.
-   **Conflict Penalty**: Conflicting sources are flagged in the UI and penalized in the final ranking.

### 5. Grounded Synthesis
The final response is generated using a strict **Grounded Prompt**:
-   The LLM must cite every claim using source markers (e.g., `[1]`).
-   It is explicitly instructed to prioritize high-confidence sources and mention source disagreements.

## Visibility & Monitoring
The entire process is visible in real-time via the **System Logs** tab in the Chat UI, which tracks every search, extraction, and evaluation event.
