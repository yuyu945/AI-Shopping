package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ToolSearchProducts         = "search_products"
	ToolGetUserProfile         = "get_user_profile"
	ToolGetPriceStock          = "get_price_stock"
	ToolGetDiscount            = "get_discount"
	ToolSearchProductKnowledge = "search_product_knowledge"
)

const (
	PermissionPublicCatalog = "public_catalog"
	PermissionCurrentUser   = "current_user"
)

// ErrUnknownTool reports a model-requested Tool outside the allow-list.
var ErrUnknownTool = errors.New("UNKNOWN_TOOL")

// ErrInvalidToolArgument reports Tool JSON that fails server-side validation.
var ErrInvalidToolArgument = errors.New("INVALID_TOOL_ARGUMENT")

// ToolDefinition declares a controlled Tool for the model provider.
type ToolDefinition struct {
	Name             string
	InputSchemaJSON  string
	Timeout          time.Duration
	MaxResults       int
	PermissionSource string
}

// ToolContext carries server-trusted request context into Tool validation.
type ToolContext struct {
	UserID uint64
}

// ToolInvocation is a validated model-requested Tool call.
type ToolInvocation struct {
	Name   string
	UserID uint64
	Args   any
}

// SearchProductsArgs is the validated input for search_products.
type SearchProductsArgs struct {
	Keyword    string `json:"keyword"`
	CategoryID uint64 `json:"category_id"`
	BudgetMin  string `json:"budget_min"`
	BudgetMax  string `json:"budget_max"`
	Limit      int    `json:"limit"`
}

// GetPriceStockArgs is the validated input for get_price_stock.
type GetPriceStockArgs struct {
	SKUIDs []uint64 `json:"sku_ids"`
}

// GetDiscountArgs is the validated input for get_discount.
type GetDiscountArgs struct {
	SKUIDs []uint64 `json:"sku_ids"`
}

// SearchProductKnowledgeArgs is the validated input for search_product_knowledge.
type SearchProductKnowledgeArgs struct {
	ProductID uint64   `json:"product_id"`
	Query     string   `json:"query"`
	DocTypes  []string `json:"doc_types"`
	TopK      int      `json:"top_k"`
}

// ToolRegistry validates model Tool requests against the allow-list.
type ToolRegistry struct {
	definitions map[string]ToolDefinition
}

// NewDefaultToolRegistry returns the M4.1 read-only Tool allow-list.
func NewDefaultToolRegistry(timeout time.Duration) *ToolRegistry {
	if timeout <= 0 {
		timeout = time.Second
	}
	return &ToolRegistry{definitions: map[string]ToolDefinition{
		ToolSearchProducts: {
			Name: ToolSearchProducts, InputSchemaJSON: searchProductsSchema, Timeout: timeout,
			MaxResults: 10, PermissionSource: PermissionPublicCatalog,
		},
		ToolGetUserProfile: {
			Name: ToolGetUserProfile, InputSchemaJSON: getUserProfileSchema, Timeout: timeout,
			MaxResults: 1, PermissionSource: PermissionCurrentUser,
		},
		ToolGetPriceStock: {
			Name: ToolGetPriceStock, InputSchemaJSON: skuIDsSchema, Timeout: timeout,
			MaxResults: 20, PermissionSource: PermissionPublicCatalog,
		},
		ToolGetDiscount: {
			Name: ToolGetDiscount, InputSchemaJSON: skuIDsSchema, Timeout: timeout,
			MaxResults: 20, PermissionSource: PermissionCurrentUser,
		},
		ToolSearchProductKnowledge: {
			Name: ToolSearchProductKnowledge, InputSchemaJSON: searchProductKnowledgeSchema, Timeout: timeout,
			MaxResults: 10, PermissionSource: PermissionPublicCatalog,
		},
	}}
}

// Definition returns a Tool definition by name.
func (r *ToolRegistry) Definition(name string) (ToolDefinition, bool) {
	if r == nil {
		return ToolDefinition{}, false
	}
	definition, ok := r.definitions[name]
	return definition, ok
}

// Validate parses and validates a model-requested Tool call.
func (r *ToolRegistry) Validate(name string, raw json.RawMessage, toolContext ToolContext) (ToolInvocation, error) {
	definition, ok := r.Definition(name)
	if !ok {
		return ToolInvocation{}, ErrUnknownTool
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	switch name {
	case ToolSearchProducts:
		var args SearchProductsArgs
		if err := decodeToolArgs(raw, &args); err != nil {
			return ToolInvocation{}, err
		}
		args.Limit = capPositive(args.Limit, 5, definition.MaxResults)
		return ToolInvocation{Name: name, UserID: toolContext.UserID, Args: args}, nil
	case ToolGetUserProfile:
		if hasModelSuppliedUserID(raw) {
			return ToolInvocation{}, ErrInvalidToolArgument
		}
		return ToolInvocation{Name: name, UserID: toolContext.UserID, Args: struct{}{}}, nil
	case ToolGetPriceStock:
		var args GetPriceStockArgs
		if err := decodeToolArgs(raw, &args); err != nil {
			return ToolInvocation{}, err
		}
		if len(args.SKUIDs) == 0 || len(args.SKUIDs) > definition.MaxResults {
			return ToolInvocation{}, ErrInvalidToolArgument
		}
		return ToolInvocation{Name: name, UserID: toolContext.UserID, Args: args}, nil
	case ToolGetDiscount:
		if hasModelSuppliedUserID(raw) {
			return ToolInvocation{}, ErrInvalidToolArgument
		}
		var args GetDiscountArgs
		if err := decodeToolArgs(raw, &args); err != nil {
			return ToolInvocation{}, err
		}
		if len(args.SKUIDs) == 0 || len(args.SKUIDs) > definition.MaxResults {
			return ToolInvocation{}, ErrInvalidToolArgument
		}
		return ToolInvocation{Name: name, UserID: toolContext.UserID, Args: args}, nil
	case ToolSearchProductKnowledge:
		var args SearchProductKnowledgeArgs
		if err := decodeToolArgs(raw, &args); err != nil {
			return ToolInvocation{}, err
		}
		args.Query = strings.TrimSpace(args.Query)
		args.TopK = capPositive(args.TopK, 5, definition.MaxResults)
		if args.ProductID == 0 || args.Query == "" {
			return ToolInvocation{}, ErrInvalidToolArgument
		}
		return ToolInvocation{Name: name, UserID: toolContext.UserID, Args: args}, nil
	default:
		return ToolInvocation{}, ErrUnknownTool
	}
}

func decodeToolArgs(raw json.RawMessage, out any) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: invalid json", ErrInvalidToolArgument)
	}
	return nil
}

func hasModelSuppliedUserID(raw json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	_, ok := fields["user_id"]
	return ok
}

func capPositive(value, defaultValue, maxValue int) int {
	if value <= 0 {
		value = defaultValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

const searchProductsSchema = `{"type":"object","properties":{"keyword":{"type":"string"},"category_id":{"type":"integer"},"budget_min":{"type":"string"},"budget_max":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":10}}}`
const getUserProfileSchema = `{"type":"object","additionalProperties":false}`
const skuIDsSchema = `{"type":"object","properties":{"sku_ids":{"type":"array","items":{"type":"integer"},"minItems":1,"maxItems":20}},"required":["sku_ids"]}`
const searchProductKnowledgeSchema = `{"type":"object","properties":{"product_id":{"type":"integer"},"query":{"type":"string"},"doc_types":{"type":"array","items":{"type":"string"}},"top_k":{"type":"integer","minimum":1,"maximum":10}},"required":["product_id","query"]}`
