// Package order implements the trade service's cart and immutable order snapshot workflow.
package order

import (
	"errors"
	"sort"
)

type Code string

const (
	InvalidArgument Code = "INVALID_ARGUMENT"
	NotFound        Code = "NOT_FOUND"
	Internal        Code = "INTERNAL"
)

// Error is a stable error that can be mapped to the service transport.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string         { return string(e.Code) + ": " + e.Message }
func IsCode(err error, code Code) bool { var e *Error; return errors.As(err, &e) && e.Code == code }

var ErrNotFound = errors.New("resource not found")

type Cart struct {
	ID, UserID uint64
	Items      []CartItem
}
type CartItem struct {
	ID, CartID, SKUID uint64
	Quantity          uint32
	Selected          bool
}
type AddCartItemInput struct {
	SKUID    uint64
	Quantity uint32
	Selected bool
}
type UpdateCartItemInput struct {
	Quantity uint32
	Selected bool
}

type OrderStatus string

const PendingPayment OrderStatus = "PENDING_PAYMENT"

type AddressSnapshot struct {
	AddressID                                                     uint64
	ReceiverName, ReceiverPhone, Province, City, District, Detail string
}
type PromotionSnapshot struct {
	PromotionID     uint64 `json:"promotion_id"`
	RuleType        string `json:"rule_type"`
	ThresholdAmount string `json:"threshold_amount,omitempty"`
	DiscountAmount  string `json:"discount_amount,omitempty"`
}
type ProductSnapshot struct {
	ProductID, SKUID      uint64
	ProductTitle, SKUCode string
	SpecJSON              []byte
	UnitPrice             string
	Saleable              bool
	Promotions            []PromotionSnapshot
}
type OrderItem struct {
	ID, OrderID, ProductID, SKUID         uint64
	ProductTitleSnapshot, SKUCodeSnapshot string
	SpecSnapshot                          []byte
	AppliedPromotion                      *PromotionSnapshot
	UnitPrice, DiscountAmount, ItemAmount string
	Quantity                              uint32
}
type Order struct {
	ID                      uint64
	OrderNo, RequestID      string
	UserID                  uint64
	Status                  OrderStatus
	TotalAmount, PaidAmount string
	Shipping                AddressSnapshot
	Items                   []OrderItem
}
type CreateOrderInput struct {
	RequestID string
	AddressID uint64
}

func cloneOrder(order Order) Order {
	result := order
	result.Shipping = order.Shipping
	result.Items = append([]OrderItem(nil), order.Items...)
	for i := range result.Items {
		result.Items[i].SpecSnapshot = append([]byte(nil), order.Items[i].SpecSnapshot...)
		if order.Items[i].AppliedPromotion != nil {
			promotion := *order.Items[i].AppliedPromotion
			result.Items[i].AppliedPromotion = &promotion
		}
	}
	return result
}

func sortedCartItems(items []CartItem) []CartItem {
	result := append([]CartItem(nil), items...)
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
