package agent

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestToolRegistryDefinesM41Tools(t *testing.T) {
	registry := NewDefaultToolRegistry(time.Second)
	for _, name := range []string{ToolSearchProducts, ToolGetUserProfile, ToolGetPriceStock, ToolGetDiscount, ToolSearchProductKnowledge} {
		definition, ok := registry.Definition(name)
		if !ok {
			t.Fatalf("Definition(%q) missing", name)
		}
		if definition.InputSchemaJSON == "" || definition.Timeout != time.Second || definition.PermissionSource == "" {
			t.Fatalf("definition(%q)=%#v", name, definition)
		}
	}
}

func TestToolRegistryRejectsUnknownTool(t *testing.T) {
	registry := NewDefaultToolRegistry(time.Second)
	_, err := registry.Validate("drop_database", json.RawMessage(`{}`), ToolContext{UserID: 42})
	if !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("Validate() error = %v, want ErrUnknownTool", err)
	}
}

func TestToolRegistryRejectsInvalidArguments(t *testing.T) {
	registry := NewDefaultToolRegistry(time.Second)
	_, err := registry.Validate(ToolGetPriceStock, json.RawMessage(`{"sku_ids":[]}`), ToolContext{UserID: 42})
	if !errors.Is(err, ErrInvalidToolArgument) {
		t.Fatalf("Validate() error = %v, want ErrInvalidToolArgument", err)
	}
}

func TestGetUserProfileRejectsModelSuppliedUserID(t *testing.T) {
	registry := NewDefaultToolRegistry(time.Second)
	_, err := registry.Validate(ToolGetUserProfile, json.RawMessage(`{"user_id":99}`), ToolContext{UserID: 42})
	if !errors.Is(err, ErrInvalidToolArgument) {
		t.Fatalf("Validate() error = %v, want ErrInvalidToolArgument", err)
	}
}

func TestToolRegistryCapsResultLimits(t *testing.T) {
	registry := NewDefaultToolRegistry(time.Second)
	search, err := registry.Validate(ToolSearchProducts, json.RawMessage(`{"keyword":"laptop","limit":99}`), ToolContext{UserID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if args := search.Args.(SearchProductsArgs); args.Limit != 10 {
		t.Fatalf("search_products limit = %d, want 10", args.Limit)
	}

	knowledge, err := registry.Validate(ToolSearchProductKnowledge, json.RawMessage(`{"product_id":1001,"query":"battery","top_k":99}`), ToolContext{UserID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if args := knowledge.Args.(SearchProductKnowledgeArgs); args.TopK != 10 {
		t.Fatalf("search_product_knowledge top_k = %d, want 10", args.TopK)
	}
}
