package order

import "testing"

func TestOrderSnapshotIntegrationConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		env       map[string]string
		wantRun   bool
		wantRunID string
		wantErr   bool
	}{
		{name: "opt in disabled skips", env: map[string]string{}},
		{name: "wrong isolated project skips", env: map[string]string{
			"AI_SHOPPING_INTEGRATION":          "1",
			"AI_SHOPPING_INTEGRATION_ISOLATED": "other",
		}},
		{name: "missing DSN is rejected", env: map[string]string{
			"AI_SHOPPING_INTEGRATION":          "1",
			"AI_SHOPPING_INTEGRATION_ISOLATED": "m21orderverify",
			"AI_SHOPPING_INTEGRATION_RUN_ID":   "ac02029b-8ff5-4f2d-8910-cb258bd78b3d",
		}, wantErr: true},
		{name: "non trade database is rejected", env: map[string]string{
			"AI_SHOPPING_INTEGRATION":          "1",
			"AI_SHOPPING_INTEGRATION_ISOLATED": "m21orderverify",
			"AI_SHOPPING_INTEGRATION_RUN_ID":   "ac02029b-8ff5-4f2d-8910-cb258bd78b3d",
			"AI_SHOPPING_MYSQL_DSN":            "app:secret@tcp(127.0.0.1:3310)/mysql",
		}, wantErr: true},
		{name: "malformed run ID is rejected", env: map[string]string{
			"AI_SHOPPING_INTEGRATION":          "1",
			"AI_SHOPPING_INTEGRATION_ISOLATED": "m21orderverify",
			"AI_SHOPPING_INTEGRATION_RUN_ID":   "invalid",
			"AI_SHOPPING_MYSQL_DSN":            "app:secret@tcp(127.0.0.1:3310)/trade_db",
		}, wantErr: true},
		{name: "isolated trade database runs", env: map[string]string{
			"AI_SHOPPING_INTEGRATION":          "1",
			"AI_SHOPPING_INTEGRATION_ISOLATED": "m21orderverify",
			"AI_SHOPPING_INTEGRATION_RUN_ID":   "ac02029b-8ff5-4f2d-8910-cb258bd78b3d",
			"AI_SHOPPING_MYSQL_DSN":            "app:secret@tcp(127.0.0.1:3310)/trade_db",
		}, wantRun: true, wantRunID: "ac02029b-8ff5-4f2d-8910-cb258bd78b3d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, run, err := orderSnapshotIntegrationConfig(func(key string) string { return tt.env[key] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("orderSnapshotIntegrationConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if run != tt.wantRun {
				t.Fatalf("orderSnapshotIntegrationConfig() run = %v, want %v", run, tt.wantRun)
			}
			if config.runID != tt.wantRunID {
				t.Fatalf("orderSnapshotIntegrationConfig() run ID = %q, want %q", config.runID, tt.wantRunID)
			}
		})
	}
}
