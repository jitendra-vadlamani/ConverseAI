# ConverseAI Frontend

The ConverseAI frontend is a high-performance React application built with Vite and TypeScript. It features a modern "Glassmorphism" UI designed for deep transparency into AI reasoning.

## 🚀 Key Features

### 1. Dual-View Chat
- **Conversation History**: Standard chat bubble view for grounded assistant responses.
- **System Logs**: Real-time analytical tracing of the **Reasoning Engine**.

### 2. Reasoning Visualization
- **Multi-Stage Tracking**: Visual iconography for Search, Extraction, and Evaluation stages.
- **Sufficiency Grid**: Transparent displays comparing "Found" vs "Missing" query aspects.
- **Precision Metrics**: Every search result is presented with **Authority**, **Freshness**, and **Final Relevance** scores.

### 3. Integrated File Management
- **Files Tab**: Centralized access to all documents uploaded or indexed in the current conversation.
- **Context Awareness**: Quick viewing of RAG-indexed status for individual files.

### 4. Interactive Citations
- **Source Badges**: Grounded claims are linked to clickable source badges that open original URLs in new tabs.
- **Conflict Warning**: Automated visual flagging for sources that contradict each other.

## 🛠 Tech Stack
- **Framework**: React 18
- **Build Tool**: Vite
- **Styling**: Vanilla CSS (Modern Modules)
- **Icons**: Lucide-React
- **Markdown**: React-Markdown + Remark-GFM (for source-badge parsing)

## 📁 Structure
- `src/components/`: Reusable UI elements (Buttons, Inputs, Modals).
- `src/pages/`: Main page views (Chat, Home).
- `src/api/`: Frontend API client for Go backend.
- `src/styles/`: Global and component-specific stylesheets.
