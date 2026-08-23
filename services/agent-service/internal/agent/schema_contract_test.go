package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentRuntimeSchemaContract(t *testing.T) {
	schema := readFile(t, filepath.Join(repoRoot(t), "deploy/mysql/init/04-agent-schema.sql"))

	assertContains(t, schema, "CREATE TABLE IF NOT EXISTS agent_sessions")
	assertContains(t, schema, "CREATE TABLE IF NOT EXISTS agent_messages")
	assertContains(t, schema, "CREATE TABLE IF NOT EXISTS agent_runs")
	assertContains(t, schema, "CREATE TABLE IF NOT EXISTS agent_steps")
	assertContains(t, schema, "UNIQUE KEY uq_agent_session_no (session_no)")
	assertContains(t, schema, "UNIQUE KEY uq_agent_message_seq (session_id, seq_no)")
	assertContains(t, schema, "UNIQUE KEY uq_agent_run_id (run_id)")
	assertContains(t, schema, "UNIQUE KEY uq_agent_step_attempt (run_id, step_no, attempt)")
	assertContains(t, schema, "CONSTRAINT chk_agent_run_status CHECK (status IN ('RUNNING','SUCCEEDED','FAILED','TIMEOUT'))")
	assertContains(t, schema, "CONSTRAINT chk_agent_step_status CHECK (status IN ('RUNNING','SUCCEEDED','FAILED','TIMEOUT'))")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertContains(t *testing.T, content, want string) {
	t.Helper()
	if !strings.Contains(content, want) {
		t.Fatalf("schema does not contain %q", want)
	}
}
