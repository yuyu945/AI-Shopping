package order

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/google/uuid"
)

type cartRepository interface {
	GetCart(context.Context, uint64) (Cart, error)
	AddCartItem(context.Context, uint64, AddCartItemInput) (CartItem, error)
	UpdateCartItem(context.Context, uint64, uint64, UpdateCartItemInput) error
	DeleteCartItem(context.Context, uint64, uint64) error
}
type orderRepository interface {
	FindOrderByRequest(context.Context, uint64, string) (Order, error)
	GetOrder(context.Context, uint64, string) (Order, error)
	ListOrders(context.Context, uint64) ([]Order, error)
	CreateOrder(context.Context, Order) (Order, error)
}
type addressReader interface {
	GetAddress(context.Context, uint64, uint64) (AddressSnapshot, error)
}
type productReader interface {
	GetProducts(context.Context, []uint64) ([]ProductSnapshot, error)
}

// Service coordinates user-scoped carts and snapshot-only orders.
type Service struct {
	cart        cartRepository
	orders      orderRepository
	addresses   addressReader
	products    productReader
	nextOrderNo func() string
}

func NewService(repository interface{}, addresses addressReader, products productReader) *Service {
	var carts cartRepository
	var orders orderRepository
	if repository != nil {
		carts, _ = repository.(cartRepository)
		orders, _ = repository.(orderRepository)
	}
	return &Service{cart: carts, orders: orders, addresses: addresses, products: products, nextOrderNo: func() string { return "ORD-" + uuid.NewString() }}
}
func (s *Service) GetCart(ctx context.Context, userID uint64) (Cart, error) {
	if userID == 0 {
		return Cart{}, invalid("user id is required")
	}
	cart, err := s.cart.GetCart(ctx, userID)
	return cart, repositoryError(err)
}
func (s *Service) AddCartItem(ctx context.Context, userID uint64, input AddCartItemInput) (CartItem, error) {
	if userID == 0 || input.SKUID == 0 || input.Quantity == 0 {
		return CartItem{}, invalid("sku_id and quantity are required")
	}
	item, err := s.cart.AddCartItem(ctx, userID, input)
	return item, repositoryError(err)
}
func (s *Service) UpdateCartItem(ctx context.Context, userID, itemID uint64, input UpdateCartItemInput) error {
	if userID == 0 || itemID == 0 || input.Quantity == 0 {
		return invalid("item id and quantity are required")
	}
	return repositoryError(s.cart.UpdateCartItem(ctx, userID, itemID, input))
}
func (s *Service) DeleteCartItem(ctx context.Context, userID, itemID uint64) error {
	if userID == 0 || itemID == 0 {
		return invalid("item id is required")
	}
	return repositoryError(s.cart.DeleteCartItem(ctx, userID, itemID))
}
func (s *Service) GetOrder(ctx context.Context, userID uint64, orderNo string) (Order, error) {
	if userID == 0 || strings.TrimSpace(orderNo) == "" {
		return Order{}, invalid("order number is required")
	}
	order, err := s.orders.GetOrder(ctx, userID, orderNo)
	return order, repositoryError(err)
}
func (s *Service) ListOrders(ctx context.Context, userID uint64) ([]Order, error) {
	if userID == 0 {
		return nil, invalid("user id is required")
	}
	orders, err := s.orders.ListOrders(ctx, userID)
	return orders, repositoryError(err)
}

func (s *Service) CreateOrder(ctx context.Context, userID uint64, input CreateOrderInput) (Order, error) {
	if userID == 0 || input.AddressID == 0 || strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 128 {
		return Order{}, invalid("request_id and address_id are required")
	}
	if existing, err := s.orders.FindOrderByRequest(ctx, userID, input.RequestID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Order{}, repositoryError(err)
	}
	address, err := s.addresses.GetAddress(ctx, userID, input.AddressID)
	if err != nil {
		return Order{}, resourceError(err)
	}
	cart, err := s.cart.GetCart(ctx, userID)
	if err != nil {
		return Order{}, repositoryError(err)
	}
	selected := make([]CartItem, 0, len(cart.Items))
	skuIDs := make([]uint64, 0, len(cart.Items))
	for _, item := range sortedCartItems(cart.Items) {
		if item.Selected && item.Quantity > 0 {
			selected = append(selected, item)
			skuIDs = append(skuIDs, item.SKUID)
		}
	}
	if len(selected) == 0 {
		return Order{}, invalid("cart has no selected items")
	}
	products, err := s.products.GetProducts(ctx, skuIDs)
	if err != nil {
		return Order{}, dependencyError(err)
	}
	productBySKU := make(map[uint64]ProductSnapshot, len(products))
	for _, product := range products {
		productBySKU[product.SKUID] = product
	}
	items := make([]OrderItem, 0, len(selected))
	total := new(big.Rat)
	for _, cartItem := range selected {
		product, ok := productBySKU[cartItem.SKUID]
		if !ok || !product.Saleable {
			return Order{}, invalid("selected product is unavailable")
		}
		item, itemAmount, err := orderItem(product, cartItem.Quantity)
		if err != nil {
			return Order{}, invalid("selected product has invalid price")
		}
		items = append(items, item)
		total.Add(total, itemAmount)
	}
	order := Order{OrderNo: s.nextOrderNo(), RequestID: input.RequestID, UserID: userID, Status: PendingPayment, TotalAmount: moneyString(total), PaidAmount: "0.00", Shipping: address, Items: items}
	created, err := s.orders.CreateOrder(ctx, order)
	return created, repositoryError(err)
}
func orderItem(product ProductSnapshot, quantity uint32) (OrderItem, *big.Rat, error) {
	unit, ok := parseMoney(product.UnitPrice)
	if !ok {
		return OrderItem{}, nil, errors.New("invalid price")
	}
	discount := new(big.Rat)
	var applied *PromotionSnapshot
	for _, promotion := range product.Promotions {
		if value, ok := parseMoney(promotion.DiscountAmount); ok && value.Cmp(discount) > 0 {
			discount = value
			selected := promotion
			applied = &selected
		}
	}
	amount := new(big.Rat).Mul(unit, new(big.Rat).SetUint64(uint64(quantity)))
	amount.Sub(amount, new(big.Rat).Mul(discount, new(big.Rat).SetUint64(uint64(quantity))))
	if amount.Sign() < 0 {
		return OrderItem{}, nil, errors.New("negative amount")
	}
	return OrderItem{ProductID: product.ProductID, SKUID: product.SKUID, ProductTitleSnapshot: product.ProductTitle, SKUCodeSnapshot: product.SKUCode, SpecSnapshot: append([]byte(nil), product.SpecJSON...), AppliedPromotion: applied, UnitPrice: moneyString(unit), DiscountAmount: moneyString(new(big.Rat).Mul(discount, new(big.Rat).SetUint64(uint64(quantity)))), Quantity: quantity, ItemAmount: moneyString(amount)}, amount, nil
}
func parseMoney(value string) (*big.Rat, bool) {
	if strings.TrimSpace(value) != value || !moneyPattern(value) {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(value)
	return r, ok
}
func moneyPattern(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(parts[1]) != 2 || len(parts[0]) < 1 || len(parts[0]) > 10 {
		return false
	}
	for _, c := range value {
		if c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
func moneyString(value *big.Rat) string {
	scaled := new(big.Rat).Mul(value, big.NewRat(100, 1))
	if !scaled.IsInt() {
		panic("money precision exceeds two decimals")
	}
	return fmt.Sprintf("%d.%02d", new(big.Int).Quo(scaled.Num(), scaled.Denom()).Uint64()/100, new(big.Int).Quo(scaled.Num(), scaled.Denom()).Uint64()%100)
}
func invalid(message string) error { return &Error{Code: InvalidArgument, Message: message} }
func resourceError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return &Error{Code: NotFound, Message: "resource not found"}
	}
	return dependencyError(err)
}
func repositoryError(err error) error {
	if err == nil {
		return nil
	}
	var stable *Error
	if errors.As(err, &stable) {
		return stable
	}
	if errors.Is(err, ErrNotFound) {
		return &Error{Code: NotFound, Message: "resource not found"}
	}
	return &Error{Code: Internal, Message: "order service failed"}
}
func dependencyError(error) error { return &Error{Code: Internal, Message: "dependency unavailable"} }
