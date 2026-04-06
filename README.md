# ConverseAI - Advanced Multimodal AI Chat

ConverseAI is a high-performance, multimodal AI chat application built with Go and React. It leverages the power of Ollama for local LLM execution, providing a secure and private environment for processing files, images, and complex reasoning tasks.

## 🚀 Features

### 1. Multimodal Input Processing
*   **Images**: Upload images for visual analysis, OCR, or contextual questioning.
*   **Documents**: Supports text-based files (`.txt`, `.md`, `.go`, etc.) and **PDFs**.
*   **Hybrid Ingestion**: Small files are injected directly into the prompt context, while larger files are automatically indexed into the **Vector Database** for semantic retrieval.

### 2. Intelligent RAG (Retrieval-Augmented Generation)
*   **Semantic Memory**: Uses **ChromaDB** with **Nomic Embed Text v2 MoE** to index and retrieve relevant snippets from your documents.
*   **Search-First Retrieval**: For every query, the system automatically checks the vector database for relevant context before generating a response with the LLM.

### 4. Intelligent Document Processing
*   **Dual-Path Extraction**:
    *   **Text-based PDFs**: Processed via high-speed direct extraction.
    *   **Scanned Documents**: Automatically handled using **DeepSeek OCR 3B** for high-accuracy text reconstruction from images and scans.

### 5. Specialized Model Routing
*   **Intelligent Planner**: Automatically selects the best model for the job:
    *   **OCR**: `deepseek-ocr:3b` for high-accuracy text extraction from images.
    *   **Vision**: `qwen3-vl:8b` for visual analysis and description.
    *   **Coding**: `qwen3-coder:latest` for technical and programming queries.
    *   **Translation**: `translategemma:12b` for multilingual support.

### 6. Efficient Resource Management
*   **Active LLM Control**: To optimize VRAM usage, only **one LLM stays active** at any point. The system automatically unloads the previous model before loading the next one.

### 7. Intelligent File Management
*   **Zero-Waste Storage**: Automatically deduplicates files using **MD5 hashing**. Identical files across different conversations share a single physical storage entry.
*   **Smart Versioning**: Intelligent filename collision handling—if you upload a new version of a file with the same name, ConverseAI automatically handles versioning to prevent data loss.
*   **Contextual Visibility**: A dedicated **Files Tab** in every conversation provides a centralized view of all documents active in that context.
*   **Reference-Aware Purging**: Deleting a file from a conversation only removes its reference. The physical file and its **Vector Index** are only purged when no other conversation references it, ensuring no "broken context" for other chats.

### 8. Durable & Scalable Storage
*   **MinIO**: Securely stores all raw images and documents.
*   **MongoDB**: Stores conversation metadata, message history, and links to stored artifacts.

## 🛠 Tech Stack
*   **Backend**: Go (Golang)
*   **Frontend**: React + Vite + TypeScript
*   **ML Engine**: Ollama (Local LLMs: Gemma 4, Qwen3, DeepSeek OCR)
*   **Primary DB**: MongoDB (NoSQL)
*   **Vector DB**: ChromaDB
*   **Object Storage**: MinIO

## ⚠️ Limitations
*   **Local Performance**: Response speed depends on local hardware (GPU/VRAM).
*   **Model Switching Latency**: Switching models introduces a small loading delay to ensure only one is active in VRAM.

## 📁 Repository Structure
*   `cmd/`: Entry points for the application.
*   `internal/handler/`: REST API handlers.
*   `internal/service/`: Business logic (Chat, RAG, Events).
*   `internal/orchestrator/`: Task planning and execution logic.
*   `internal/manager/`: Resource and model lifecycle management.
*   `internal/repository/`: Data persistence (MongoDB, File Storage).
*   `client/`: Frontend React application.
