package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckOllamaModelStatus(t *testing.T) {
	tests := []struct {
		name           string
		modelName      string
		mockResponse   interface{}
		mockStatusCode int
		expectedStatus string
	}{
		{
			name:      "Exact Match",
			modelName: "qwen3.5:0.8b",
			mockResponse: map[string]interface{}{
				"models": []map[string]string{
					{"name": "qwen3.5:0.8b"},
					{"name": "llama3:latest"},
				},
			},
			mockStatusCode: http.StatusOK,
			expectedStatus: "Online",
		},
		{
			name:      "Case Sensitive Mismatch",
			modelName: "Qwen3.5:0.8b",
			mockResponse: map[string]interface{}{
				"models": []map[string]string{
					{"name": "qwen3.5:0.8b"},
				},
			},
			mockStatusCode: http.StatusOK,
			expectedStatus: "Missing",
		},
		{
			name:      "Tag Mismatch",
			modelName: "qwen3.5",
			mockResponse: map[string]interface{}{
				"models": []map[string]string{
					{"name": "qwen3.5:0.8b"},
				},
			},
			mockStatusCode: http.StatusOK,
			expectedStatus: "Missing",
		},
		{
			name:           "Server Error",
			modelName:      "any",
			mockResponse:   nil,
			mockStatusCode: http.StatusInternalServerError,
			expectedStatus: "Offline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/tags" {
					t.Errorf("Expected path /api/tags, got %s", r.URL.Path)
				}
				w.WriteHeader(tt.mockStatusCode)
				if tt.mockResponse != nil {
					json.NewEncoder(w).Encode(tt.mockResponse)
				}
			}))
			defer server.Close()

			s := &llmService{
				httpClient: server.Client(),
			}

			status := s.checkOllamaModelStatus(server.URL, tt.modelName)
			if status != tt.expectedStatus {
				t.Errorf("expected %s, got %s", tt.expectedStatus, status)
			}
		})
	}
}
