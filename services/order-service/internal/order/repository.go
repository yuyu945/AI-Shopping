package order

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/go-sql-driver/mysql"
)

const queryCart = `SELECT id, user_id FROM carts WHERE user_id = ?`
const queryCartItems = `SELECT id, cart_id, sku_id, quantity, selected FROM cart_items WHERE cart_id = ? ORDER BY id`
const insertCart = `INSERT INTO carts (user_id) VALUES (?) ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`
const queryCartItemForUpdate = `SELECT id, quantity FROM cart_items WHERE cart_id = ? AND sku_id = ? FOR UPDATE`
const insertCartItem = `INSERT INTO cart_items (cart_id, sku_id, quantity, selected) VALUES (?, ?, ?, ?)`
const increaseCartItem = `UPDATE cart_items SET quantity = ?, selected = ? WHERE id = ?`
const updateCartItem = `UPDATE cart_items AS item INNER JOIN carts AS cart ON cart.id = item.cart_id SET item.quantity = ?, item.selected = ? WHERE item.id = ? AND cart.user_id = ?`
const deleteCartItem = `DELETE item FROM cart_items AS item INNER JOIN carts AS cart ON cart.id = item.cart_id WHERE item.id = ? AND cart.user_id = ?`
const queryOrderByRequest = `SELECT id, order_no, request_id, user_id, status, total_amount, paid_amount, shipping_name_snapshot, shipping_phone_snapshot, shipping_address_snapshot FROM orders WHERE user_id = ? AND request_id = ?`
const queryOrderByNumber = `SELECT id, order_no, request_id, user_id, status, total_amount, paid_amount, shipping_name_snapshot, shipping_phone_snapshot, shipping_address_snapshot FROM orders WHERE user_id = ? AND order_no = ?`
const queryOrders = `SELECT id, order_no, request_id, user_id, status, total_amount, paid_amount, shipping_name_snapshot, shipping_phone_snapshot, shipping_address_snapshot FROM orders WHERE user_id = ? ORDER BY created_at DESC, id DESC`
const queryOrderItems = `SELECT id, order_id, product_id, sku_id, product_title_snapshot, sku_code_snapshot, sku_spec_snapshot, promotion_snapshot, unit_price, discount_amount, quantity, item_amount FROM order_items WHERE order_id = ? ORDER BY id`
const insertOrder = `INSERT INTO orders (order_no, user_id, request_id, status, total_amount, paid_amount, shipping_name_snapshot, shipping_phone_snapshot, shipping_address_snapshot) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
const insertOrderItem = `INSERT INTO order_items (order_id, product_id, sku_id, product_title_snapshot, sku_code_snapshot, sku_spec_snapshot, promotion_snapshot, unit_price, discount_amount, quantity, item_amount) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// MySQLRepository persists only trade_db carts and immutable order snapshots.
type MySQLRepository struct{ db *sql.DB }

// NewMySQLRepository creates a trade_db repository backed by db.
func NewMySQLRepository(db *sql.DB) *MySQLRepository { return &MySQLRepository{db: db} }

func (r *MySQLRepository) GetCart(ctx context.Context, userID uint64) (Cart, error) {
	var cart Cart
	err := r.db.QueryRowContext(ctx, queryCart, userID).Scan(&cart.ID, &cart.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return Cart{UserID: userID, Items: []CartItem{}}, nil
	}
	if err != nil {
		return Cart{}, fmt.Errorf("get cart: %w", err)
	}
	items, err := r.loadCartItems(ctx, cart.ID)
	if err != nil {
		return Cart{}, err
	}
	cart.Items = items
	return cart, nil
}

func (r *MySQLRepository) AddCartItem(ctx context.Context, userID uint64, input AddCartItemInput) (item CartItem, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return CartItem{}, fmt.Errorf("begin cart item transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	result, err := tx.ExecContext(ctx, insertCart, userID)
	if err != nil {
		return CartItem{}, fmt.Errorf("upsert cart: %w", err)
	}
	cartID, err := result.LastInsertId()
	if err != nil {
		return CartItem{}, fmt.Errorf("read cart id: %w", err)
	}
	var existingID uint64
	var quantity uint32
	err = tx.QueryRowContext(ctx, queryCartItemForUpdate, cartID, input.SKUID).Scan(&existingID, &quantity)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		result, err = tx.ExecContext(ctx, insertCartItem, cartID, input.SKUID, input.Quantity, input.Selected)
		if err != nil {
			return CartItem{}, fmt.Errorf("insert cart item: %w", err)
		}
		id, lastErr := result.LastInsertId()
		if lastErr != nil {
			return CartItem{}, fmt.Errorf("read cart item id: %w", lastErr)
		}
		item = CartItem{ID: uint64(id), CartID: uint64(cartID), SKUID: input.SKUID, Quantity: input.Quantity, Selected: input.Selected}
	case err != nil:
		return CartItem{}, fmt.Errorf("lock cart item: %w", err)
	case uint64(quantity)+uint64(input.Quantity) > math.MaxUint32:
		return CartItem{}, invalid("cart item quantity exceeds limit")
	default:
		quantity += input.Quantity
		if _, err = tx.ExecContext(ctx, increaseCartItem, quantity, input.Selected, existingID); err != nil {
			return CartItem{}, fmt.Errorf("increase cart item: %w", err)
		}
		item = CartItem{ID: existingID, CartID: uint64(cartID), SKUID: input.SKUID, Quantity: quantity, Selected: input.Selected}
	}
	if err = tx.Commit(); err != nil {
		return CartItem{}, fmt.Errorf("commit cart item transaction: %w", err)
	}
	return item, nil
}

func (r *MySQLRepository) UpdateCartItem(ctx context.Context, userID, itemID uint64, input UpdateCartItemInput) error {
	result, err := r.db.ExecContext(ctx, updateCartItem, input.Quantity, input.Selected, itemID, userID)
	if err != nil {
		return fmt.Errorf("update cart item: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated cart item rows: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MySQLRepository) DeleteCartItem(ctx context.Context, userID, itemID uint64) error {
	result, err := r.db.ExecContext(ctx, deleteCartItem, itemID, userID)
	if err != nil {
		return fmt.Errorf("delete cart item: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted cart item rows: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MySQLRepository) FindOrderByRequest(ctx context.Context, userID uint64, requestID string) (Order, error) {
	return r.findOne(ctx, queryOrderByRequest, userID, requestID)
}

func (r *MySQLRepository) GetOrder(ctx context.Context, userID uint64, orderNo string) (Order, error) {
	return r.findOne(ctx, queryOrderByNumber, userID, orderNo)
}

func (r *MySQLRepository) ListOrders(ctx context.Context, userID uint64) ([]Order, error) {
	rows, err := r.db.QueryContext(ctx, queryOrders, userID)
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()
	orders := []Order{}
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		order.Items, err = r.loadOrderItems(ctx, order.ID)
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orders: %w", err)
	}
	return orders, nil
}

func (r *MySQLRepository) CreateOrder(ctx context.Context, order Order) (created Order, err error) {
	if existing, findErr := r.FindOrderByRequest(ctx, order.UserID, order.RequestID); findErr == nil {
		return existing, nil
	} else if !errors.Is(findErr, ErrNotFound) {
		return Order{}, findErr
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, fmt.Errorf("begin order transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	addressJSON, err := marshalAddress(order.Shipping)
	if err != nil {
		return Order{}, err
	}
	result, err := tx.ExecContext(ctx, insertOrder, order.OrderNo, order.UserID, order.RequestID, order.Status, order.TotalAmount, order.PaidAmount, order.Shipping.ReceiverName, order.Shipping.ReceiverPhone, addressJSON)
	if err != nil {
		if isDuplicate(err) {
			_ = tx.Rollback()
			return r.FindOrderByRequest(ctx, order.UserID, order.RequestID)
		}
		return Order{}, fmt.Errorf("insert order: %w", err)
	}
	orderID, err := result.LastInsertId()
	if err != nil {
		return Order{}, fmt.Errorf("read order id: %w", err)
	}
	created = cloneOrder(order)
	created.ID = uint64(orderID)
	for index := range created.Items {
		item := &created.Items[index]
		promotionJSON, marshalErr := marshalPromotion(item.AppliedPromotion)
		if marshalErr != nil {
			err = marshalErr
			return Order{}, err
		}
		itemResult, execErr := tx.ExecContext(ctx, insertOrderItem, orderID, item.ProductID, item.SKUID, item.ProductTitleSnapshot, item.SKUCodeSnapshot, item.SpecSnapshot, promotionJSON, item.UnitPrice, item.DiscountAmount, item.Quantity, item.ItemAmount)
		if execErr != nil {
			err = fmt.Errorf("insert order item: %w", execErr)
			return Order{}, err
		}
		itemID, idErr := itemResult.LastInsertId()
		if idErr != nil {
			err = fmt.Errorf("read order item id: %w", idErr)
			return Order{}, err
		}
		item.ID, item.OrderID = uint64(itemID), uint64(orderID)
	}
	if err = tx.Commit(); err != nil {
		return Order{}, fmt.Errorf("commit order transaction: %w", err)
	}
	return cloneOrder(created), nil
}

func (r *MySQLRepository) loadCartItems(ctx context.Context, cartID uint64) ([]CartItem, error) {
	rows, err := r.db.QueryContext(ctx, queryCartItems, cartID)
	if err != nil {
		return nil, fmt.Errorf("list cart items: %w", err)
	}
	defer rows.Close()
	items := []CartItem{}
	for rows.Next() {
		var item CartItem
		if err := rows.Scan(&item.ID, &item.CartID, &item.SKUID, &item.Quantity, &item.Selected); err != nil {
			return nil, fmt.Errorf("scan cart item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cart items: %w", err)
	}
	return items, nil
}

func (r *MySQLRepository) findOne(ctx context.Context, query string, userID uint64, key string) (Order, error) {
	row := r.db.QueryRowContext(ctx, query, userID, key)
	order, err := scanOrder(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	order.Items, err = r.loadOrderItems(ctx, order.ID)
	if err != nil {
		return Order{}, err
	}
	return cloneOrder(order), nil
}

type scanner interface{ Scan(...any) error }

func scanOrder(row scanner) (Order, error) {
	var order Order
	var addressJSON []byte
	if err := row.Scan(&order.ID, &order.OrderNo, &order.RequestID, &order.UserID, &order.Status, &order.TotalAmount, &order.PaidAmount, &order.Shipping.ReceiverName, &order.Shipping.ReceiverPhone, &addressJSON); err != nil {
		return Order{}, err
	}
	if _, ok := parseMoney(order.TotalAmount); !ok {
		return Order{}, errors.New("stored total amount is invalid")
	}
	if _, ok := parseMoney(order.PaidAmount); !ok {
		return Order{}, errors.New("stored paid amount is invalid")
	}
	if err := json.Unmarshal(addressJSON, &storedAddress{}); err != nil {
		return Order{}, errors.New("stored shipping address is invalid")
	}
	var stored storedAddress
	if err := json.Unmarshal(addressJSON, &stored); err != nil {
		return Order{}, errors.New("stored shipping address is invalid")
	}
	order.Shipping.Province, order.Shipping.City, order.Shipping.District, order.Shipping.Detail = stored.Province, stored.City, stored.District, stored.Detail
	return order, nil
}

func (r *MySQLRepository) loadOrderItems(ctx context.Context, orderID uint64) ([]OrderItem, error) {
	rows, err := r.db.QueryContext(ctx, queryOrderItems, orderID)
	if err != nil {
		return nil, fmt.Errorf("list order items: %w", err)
	}
	defer rows.Close()
	items := []OrderItem{}
	for rows.Next() {
		var item OrderItem
		var promotionJSON []byte
		if err := rows.Scan(&item.ID, &item.OrderID, &item.ProductID, &item.SKUID, &item.ProductTitleSnapshot, &item.SKUCodeSnapshot, &item.SpecSnapshot, &promotionJSON, &item.UnitPrice, &item.DiscountAmount, &item.Quantity, &item.ItemAmount); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		if !json.Valid(item.SpecSnapshot) || !validItemMoney(item) {
			return nil, errors.New("stored order item snapshot is invalid")
		}
		if string(promotionJSON) != "null" {
			var promotion PromotionSnapshot
			if err := json.Unmarshal(promotionJSON, &promotion); err != nil || !validPromotion(promotion) {
				return nil, errors.New("stored promotion snapshot is invalid")
			}
			item.AppliedPromotion = &promotion
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order items: %w", err)
	}
	return items, nil
}

func validItemMoney(item OrderItem) bool {
	_, unit := parseMoney(item.UnitPrice)
	_, discount := parseMoney(item.DiscountAmount)
	_, amount := parseMoney(item.ItemAmount)
	return unit && discount && amount
}
func validPromotion(p PromotionSnapshot) bool {
	if p.PromotionID == 0 || p.RuleType == "" {
		return false
	}
	if p.ThresholdAmount != "" {
		if _, ok := parseMoney(p.ThresholdAmount); !ok {
			return false
		}
	}
	if p.DiscountAmount != "" {
		if _, ok := parseMoney(p.DiscountAmount); !ok {
			return false
		}
	}
	return true
}

type storedAddress struct {
	Province string `json:"province"`
	City     string `json:"city"`
	District string `json:"district"`
	Detail   string `json:"detail"`
}

func marshalAddress(address AddressSnapshot) ([]byte, error) {
	return json.Marshal(storedAddress{Province: address.Province, City: address.City, District: address.District, Detail: address.Detail})
}
func marshalPromotion(promotion *PromotionSnapshot) ([]byte, error) {
	if promotion == nil {
		return []byte("null"), nil
	}
	if !validPromotion(*promotion) {
		return nil, errors.New("invalid applied promotion")
	}
	return json.Marshal(promotion)
}
func isDuplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
