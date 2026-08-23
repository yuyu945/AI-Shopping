package knowledge

import "testing"

func TestKnowledgeIntegrationConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		env       map[string]string
		wantRun   bool
		wantRunID string
		wantError bool
	}{
		{
			name:    "integration switch disabled skips",
			env:     map[string]string{},
			wantRun: false,
		},
		{
			name: "isolation sentinel mismatch skips",
			env: map[string]string{
				"AI_SHOPPING_INTEGRATION":          "1",
				"AI_SHOPPING_INTEGRATION_ISOLATED": "other",
			},
			wantRun: false,
		},
		{
			name: "missing required dependencies is rejected",
			env: map[string]string{
				"AI_SHOPPING_INTEGRATION":          "1",
				"AI_SHOPPING_INTEGRATION_ISOLATED": "m32knowledge",
				"AI_SHOPPING_INTEGRATION_RUN_ID":   "79a5b82e-79bf-4f76-9b9d-3a5f478b5d29",
			},
			wantError: true,
		},
		{
			name: "default MySQL port is rejected",
			env: map[string]string{
				"AI_SHOPPING_INTEGRATION":          "1",
				"AI_SHOPPING_INTEGRATION_ISOLATED": "m32knowledge",
				"AI_SHOPPING_INTEGRATION_RUN_ID":   "79a5b82e-79bf-4f76-9b9d-3a5f478b5d29",
				"AI_SHOPPING_MYSQL_DSN":            "app:secret@tcp(127.0.0.1:3306)/knowledge_db",
				"AI_SHOPPING_MINIO_ENDPOINT":       "127.0.0.1:9100",
				"AI_SHOPPING_MINIO_ACCESS_KEY":     "minioadmin",
				"AI_SHOPPING_MINIO_SECRET_KEY":     "minioadminsecret",
				"AI_SHOPPING_KAFKA_BROKERS":        "127.0.0.1:29092",
				"AI_SHOPPING_MILVUS_ADDRESS":       "http://127.0.0.1:19530",
				"AI_SHOPPING_DASHSCOPE_API_KEY":    "dashscope-key",
			},
			wantError: true,
		},
		{
			name: "non knowledge database is rejected",
			env: map[string]string{
				"AI_SHOPPING_INTEGRATION":          "1",
				"AI_SHOPPING_INTEGRATION_ISOLATED": "m32knowledge",
				"AI_SHOPPING_INTEGRATION_RUN_ID":   "79a5b82e-79bf-4f76-9b9d-3a5f478b5d29",
				"AI_SHOPPING_MYSQL_DSN":            "app:secret@tcp(127.0.0.1:33306)/trade_db",
				"AI_SHOPPING_MINIO_ENDPOINT":       "127.0.0.1:9100",
				"AI_SHOPPING_MINIO_ACCESS_KEY":     "minioadmin",
				"AI_SHOPPING_MINIO_SECRET_KEY":     "minioadminsecret",
				"AI_SHOPPING_KAFKA_BROKERS":        "127.0.0.1:29092",
				"AI_SHOPPING_MILVUS_ADDRESS":       "http://127.0.0.1:19530",
				"AI_SHOPPING_DASHSCOPE_API_KEY":    "dashscope-key",
			},
			wantError: true,
		},
		{
			name: "isolated configuration is accepted",
			env: map[string]string{
				"AI_SHOPPING_INTEGRATION":          "1",
				"AI_SHOPPING_INTEGRATION_ISOLATED": "m32knowledge",
				"AI_SHOPPING_INTEGRATION_RUN_ID":   "79a5b82e-79bf-4f76-9b9d-3a5f478b5d29",
				"AI_SHOPPING_MYSQL_DSN":            "app:secret@tcp(127.0.0.1:33306)/knowledge_db",
				"AI_SHOPPING_MINIO_ENDPOINT":       "127.0.0.1:9100",
				"AI_SHOPPING_MINIO_ACCESS_KEY":     "minioadmin",
				"AI_SHOPPING_MINIO_SECRET_KEY":     "minioadminsecret",
				"AI_SHOPPING_KAFKA_BROKERS":        "127.0.0.1:29092",
				"AI_SHOPPING_MILVUS_ADDRESS":       "http://127.0.0.1:19530",
				"AI_SHOPPING_DASHSCOPE_API_KEY":    "dashscope-key",
			},
			wantRun:   true,
			wantRunID: "79a5b82e-79bf-4f76-9b9d-3a5f478b5d29",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, run, err := knowledgeIntegrationConfig(func(key string) string {
				return tt.env[key]
			})
			if (err != nil) != tt.wantError {
				t.Fatalf("knowledgeIntegrationConfig() error = %v, wantError %v", err, tt.wantError)
			}
			if run != tt.wantRun {
				t.Fatalf("knowledgeIntegrationConfig() run = %v, want %v", run, tt.wantRun)
			}
			if config.runID != tt.wantRunID {
				t.Fatalf("knowledgeIntegrationConfig() run ID = %q, want %q", config.runID, tt.wantRunID)
			}
		})
	}
}
