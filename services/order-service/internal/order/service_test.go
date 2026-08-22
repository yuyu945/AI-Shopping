package order

import (
	"context"
	"errors"
	"testing"
)

func TestCartServiceUserIsolation(t *testing.T) {
	repo := newMemoryRepository()
	service := NewService(repo, nil, nil)

	item, err := service.AddCartItem(context.Background(), 7, AddCartItemInput{SKUID: 101, Quantity: 2, Selected: true})
	if err != nil {
		t.Fatalf("add cart item: %v", err)
	}
	if err := service.UpdateCartItem(context.Background(), 8, item.ID, UpdateCartItemInput{Quantity: 3, Selected: true}); !IsCode(err, NotFound) {
		t.Fatalf("cross-user update error = %v, want NOT_FOUND", err)
	}
	if err := service.DeleteCartItem(context.Background(), 8, item.ID); !IsCode(err, NotFound) {
		t.Fatalf("cross-user delete error = %v, want NOT_FOUND", err)
	}
	cart, err := service.GetCart(context.Background(), 7)
	if err != nil || len(cart.Items) != 1 || cart.Items[0].Quantity != 2 {
		t.Fatalf("owner cart = %#v, %v; want unchanged item", cart, err)
	}
}

func TestCreateOrder(t *testing.T) {
	validAddress := AddressSnapshot{AddressID: 11, ReceiverName: "Ada", ReceiverPhone: "13800138000", Province: "Zhejiang", City: "Hangzhou", District: "Xihu", Detail: "No. 1"}
	validProduct := ProductSnapshot{ProductID: 21, SKUID: 101, ProductTitle: "Keyboard", SKUCode: "KB-1", SpecJSON: []byte(`{"color":"black"}`), UnitPrice: "99.00", Saleable: true, Promotions: []PromotionSnapshot{{PromotionID: 5, RuleType: "DIRECT", DiscountAmount: "10.00"}}}

	cases := []struct {
		name       string
		setup      func(*memoryRepository)
		input      CreateOrderInput
		wantCode   Code
		wantAmount string
	}{
		{name: "empty cart", input: CreateOrderInput{RequestID: "request-1", AddressID: 11}, wantCode: InvalidArgument},
		{name: "address not owned", setup: func(r *memoryRepository) {
			r.items[1] = CartItem{ID: 1, CartID: r.cart(7), SKUID: 101, Quantity: 1, Selected: true}
		}, input: CreateOrderInput{RequestID: "request-1", AddressID: 12}, wantCode: NotFound},
		{name: "product no longer saleable", setup: func(r *memoryRepository) {
			r.items[1] = CartItem{ID: 1, CartID: r.cart(7), SKUID: 101, Quantity: 1, Selected: true}
			r.product = ProductSnapshot{SKUID: 101, Saleable: false}
		}, input: CreateOrderInput{RequestID: "request-1", AddressID: 11}, wantCode: InvalidArgument},
		{name: "writes immutable snapshots", setup: func(r *memoryRepository) {
			r.items[1] = CartItem{ID: 1, CartID: r.cart(7), SKUID: 101, Quantity: 2, Selected: true}
		}, input: CreateOrderInput{RequestID: "request-1", AddressID: 11}, wantAmount: "178.00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemoryRepository()
			repo.address = validAddress
			repo.product = validProduct
			if tc.setup != nil {
				tc.setup(repo)
			}
			service := NewService(repo, repo, repo)
			result, err := service.CreateOrder(context.Background(), 7, tc.input)
			if tc.wantCode != "" {
				if !IsCode(err, tc.wantCode) {
					t.Fatalf("error = %v, want %s", err, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("create order: %v", err)
			}
			if result.Status != PendingPayment || result.TotalAmount != tc.wantAmount {
				t.Fatalf("order = %#v", result)
			}
			if len(result.Items) != 1 || result.Items[0].ProductTitleSnapshot != "Keyboard" || string(result.Items[0].SpecSnapshot) != `{"color":"black"}` || result.Items[0].DiscountAmount != "20.00" || result.Items[0].AppliedPromotion == nil || result.Items[0].AppliedPromotion.PromotionID != 5 || len(result.Items[0].CandidatePromotions) != 1 {
				t.Fatalf("order snapshots = %#v", result.Items)
			}
			repo.product.ProductTitle = "Changed"
			repo.product.UnitPrice = "1.00"
			repo.product.Promotions[0].DiscountAmount = "1.00"
			loaded, err := service.GetOrder(context.Background(), 7, result.OrderNo)
			if err != nil || loaded.Items[0].ProductTitleSnapshot != "Keyboard" || loaded.Items[0].UnitPrice != "99.00" || loaded.Items[0].CandidatePromotions[0].DiscountAmount != "10.00" || loaded.Items[0].AppliedPromotion == nil || loaded.Items[0].AppliedPromotion.DiscountAmount != "10.00" {
				t.Fatalf("loaded order = %#v, %v", loaded, err)
			}
		})
	}
}

func TestCreateOrderIdempotency(t *testing.T) {
	repo := newMemoryRepository()
	repo.address = AddressSnapshot{AddressID: 11, ReceiverName: "Ada", ReceiverPhone: "13800138000", Province: "Zhejiang", City: "Hangzhou", District: "Xihu", Detail: "No. 1"}
	repo.product = ProductSnapshot{ProductID: 21, SKUID: 101, ProductTitle: "Keyboard", SKUCode: "KB-1", SpecJSON: []byte(`{}`), UnitPrice: "99.00", Saleable: true}
	repo.items[1] = CartItem{ID: 1, CartID: repo.cart(7), SKUID: 101, Quantity: 1, Selected: true}
	service := NewService(repo, repo, repo)
	first, err := service.CreateOrder(context.Background(), 7, CreateOrderInput{RequestID: "request-1", AddressID: 11})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := service.CreateOrder(context.Background(), 7, CreateOrderInput{RequestID: "request-1", AddressID: 11})
	if err != nil || first.OrderNo != second.OrderNo || repo.createdOrders != 1 {
		t.Fatalf("second = %#v, %v, creates=%d", second, err, repo.createdOrders)
	}
}

func TestCreateOrderGeneratesDistinctOrderNumbers(t *testing.T) {
	repo := newMemoryRepository()
	repo.address = AddressSnapshot{AddressID: 11, ReceiverName: "Ada", ReceiverPhone: "13800138000", Province: "Zhejiang", City: "Hangzhou", District: "Xihu", Detail: "No. 1"}
	repo.product = ProductSnapshot{ProductID: 21, SKUID: 101, ProductTitle: "Keyboard", SKUCode: "KB-1", SpecJSON: []byte(`{}`), UnitPrice: "99.00", Saleable: true}
	repo.items[1] = CartItem{ID: 1, CartID: repo.cart(7), SKUID: 101, Quantity: 1, Selected: true}
	service := NewService(repo, repo, repo)

	first, err := service.CreateOrder(context.Background(), 7, CreateOrderInput{RequestID: "request-1", AddressID: 11})
	if err != nil {
		t.Fatalf("first CreateOrder: %v", err)
	}
	second, err := service.CreateOrder(context.Background(), 7, CreateOrderInput{RequestID: "request-2", AddressID: 11})
	if err != nil {
		t.Fatalf("second CreateOrder: %v", err)
	}
	if first.OrderNo == second.OrderNo {
		t.Fatalf("order numbers = %q and %q, want distinct values", first.OrderNo, second.OrderNo)
	}
}

func TestOrderItemAppliesOnlyEligiblePromotions(t *testing.T) {
	cases := []struct {
		name            string
		promotions      []PromotionSnapshot
		wantPromotionID uint64
		wantDiscount    string
		wantItemAmount  string
	}{
		{
			name:           "threshold not met",
			promotions:     []PromotionSnapshot{{PromotionID: 1, RuleType: "DIRECT", ThresholdAmount: "300.00", DiscountAmount: "50.00"}},
			wantDiscount:   "0.00",
			wantItemAmount: "200.00",
		},
		{
			name:            "threshold exactly met",
			promotions:      []PromotionSnapshot{{PromotionID: 1, RuleType: "DIRECT", ThresholdAmount: "200.00", DiscountAmount: "10.00"}},
			wantPromotionID: 1,
			wantDiscount:    "20.00",
			wantItemAmount:  "180.00",
		},
		{
			name: "highest discount is ineligible",
			promotions: []PromotionSnapshot{
				{PromotionID: 1, RuleType: "DIRECT", ThresholdAmount: "300.00", DiscountAmount: "90.00"},
				{PromotionID: 2, RuleType: "DIRECT", DiscountAmount: "20.00"},
			},
			wantPromotionID: 2,
			wantDiscount:    "40.00",
			wantItemAmount:  "160.00",
		},
		{
			name: "same discount picks lowest promotion id",
			promotions: []PromotionSnapshot{
				{PromotionID: 9, RuleType: "DIRECT", DiscountAmount: "10.00"},
				{PromotionID: 3, RuleType: "DIRECT", DiscountAmount: "10.00"},
			},
			wantPromotionID: 3,
			wantDiscount:    "20.00",
			wantItemAmount:  "180.00",
		},
		{name: "no promotion", wantDiscount: "0.00", wantItemAmount: "200.00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item, amount, err := orderItem(ProductSnapshot{ProductID: 21, SKUID: 101, ProductTitle: "Keyboard", SKUCode: "KB-1", SpecJSON: []byte(`{}`), UnitPrice: "100.00", Saleable: true, Promotions: tc.promotions}, 2)
			if err != nil {
				t.Fatalf("orderItem: %v", err)
			}
			if item.DiscountAmount != tc.wantDiscount || item.ItemAmount != tc.wantItemAmount || moneyString(amount) != tc.wantItemAmount {
				t.Fatalf("item = %#v, amount = %s", item, moneyString(amount))
			}
			if tc.wantPromotionID == 0 {
				if item.AppliedPromotion != nil {
					t.Fatalf("applied promotion = %#v, want nil", item.AppliedPromotion)
				}
				return
			}
			if item.AppliedPromotion == nil || item.AppliedPromotion.PromotionID != tc.wantPromotionID {
				t.Fatalf("applied promotion = %#v, want %d", item.AppliedPromotion, tc.wantPromotionID)
			}
		})
	}
}

func TestCreateOrderRejectsInvalidPromotionMoney(t *testing.T) {
	cases := []struct {
		name      string
		promotion PromotionSnapshot
	}{
		{name: "invalid threshold", promotion: PromotionSnapshot{PromotionID: 1, RuleType: "DIRECT", ThresholdAmount: "one hundred", DiscountAmount: "10.00"}},
		{name: "invalid discount", promotion: PromotionSnapshot{PromotionID: 1, RuleType: "DIRECT", DiscountAmount: "10"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemoryRepository()
			repo.address = AddressSnapshot{AddressID: 11, ReceiverName: "Ada", ReceiverPhone: "13800138000", Province: "Zhejiang", City: "Hangzhou", District: "Xihu", Detail: "No. 1"}
			repo.product = ProductSnapshot{ProductID: 21, SKUID: 101, ProductTitle: "Keyboard", SKUCode: "KB-1", SpecJSON: []byte(`{}`), UnitPrice: "99.00", Saleable: true, Promotions: []PromotionSnapshot{tc.promotion}}
			repo.items[1] = CartItem{ID: 1, CartID: repo.cart(7), SKUID: 101, Quantity: 1, Selected: true}

			_, err := NewService(repo, repo, repo).CreateOrder(context.Background(), 7, CreateOrderInput{RequestID: "request-1", AddressID: 11})
			if !IsCode(err, InvalidArgument) {
				t.Fatalf("CreateOrder error = %v, want INVALID_ARGUMENT", err)
			}
		})
	}
}

type memoryRepository struct {
	carts                         map[uint64]uint64
	items                         map[uint64]CartItem
	orders                        map[string]Order
	address                       AddressSnapshot
	product                       ProductSnapshot
	nextCart, nextItem, nextOrder uint64
	createdOrders                 int
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{carts: map[uint64]uint64{}, items: map[uint64]CartItem{}, orders: map[string]Order{}, nextCart: 1, nextItem: 1, nextOrder: 1}
}
func (r *memoryRepository) cart(userID uint64) uint64 {
	if id := r.carts[userID]; id != 0 {
		return id
	}
	id := r.nextCart
	r.nextCart++
	r.carts[userID] = id
	return id
}
func (r *memoryRepository) GetCart(_ context.Context, userID uint64) (Cart, error) {
	c := Cart{ID: r.carts[userID], UserID: userID}
	for _, i := range r.items {
		if i.CartID == c.ID {
			c.Items = append(c.Items, i)
		}
	}
	return c, nil
}
func (r *memoryRepository) AddCartItem(_ context.Context, userID uint64, in AddCartItemInput) (CartItem, error) {
	c := r.cart(userID)
	for id, i := range r.items {
		if i.CartID == c && i.SKUID == in.SKUID {
			i.Quantity += in.Quantity
			i.Selected = in.Selected
			r.items[id] = i
			return i, nil
		}
	}
	i := CartItem{ID: r.nextItem, CartID: c, SKUID: in.SKUID, Quantity: in.Quantity, Selected: in.Selected}
	r.nextItem++
	r.items[i.ID] = i
	return i, nil
}
func (r *memoryRepository) UpdateCartItem(_ context.Context, userID, itemID uint64, in UpdateCartItemInput) error {
	i, ok := r.items[itemID]
	if !ok || i.CartID != r.carts[userID] {
		return ErrNotFound
	}
	i.Quantity, i.Selected = in.Quantity, in.Selected
	r.items[itemID] = i
	return nil
}
func (r *memoryRepository) DeleteCartItem(_ context.Context, userID, itemID uint64) error {
	i, ok := r.items[itemID]
	if !ok || i.CartID != r.carts[userID] {
		return ErrNotFound
	}
	delete(r.items, itemID)
	return nil
}
func (r *memoryRepository) GetOrder(_ context.Context, userID uint64, orderNo string) (Order, error) {
	o, ok := r.orders[orderNo]
	if !ok || o.UserID != userID {
		return Order{}, ErrNotFound
	}
	return cloneOrder(o), nil
}
func (r *memoryRepository) FindOrderByRequest(_ context.Context, userID uint64, requestID string) (Order, error) {
	for _, order := range r.orders {
		if order.UserID == userID && order.RequestID == requestID {
			return cloneOrder(order), nil
		}
	}
	return Order{}, ErrNotFound
}
func (r *memoryRepository) ListOrders(_ context.Context, userID uint64) ([]Order, error) {
	out := []Order{}
	for _, o := range r.orders {
		if o.UserID == userID {
			out = append(out, cloneOrder(o))
		}
	}
	return out, nil
}
func (r *memoryRepository) CreateOrder(_ context.Context, order Order) (Order, error) {
	for _, existing := range r.orders {
		if existing.UserID == order.UserID && existing.RequestID == order.RequestID {
			return cloneOrder(existing), nil
		}
	}
	order.ID = r.nextOrder
	r.nextOrder++
	r.orders[order.OrderNo] = cloneOrder(order)
	r.createdOrders++
	return order, nil
}
func (r *memoryRepository) GetAddress(_ context.Context, userID, addressID uint64) (AddressSnapshot, error) {
	if userID != 7 || addressID != r.address.AddressID {
		return AddressSnapshot{}, ErrNotFound
	}
	return r.address, nil
}
func (r *memoryRepository) GetProducts(_ context.Context, skuIDs []uint64) ([]ProductSnapshot, error) {
	if len(skuIDs) != 1 || skuIDs[0] != r.product.SKUID {
		return nil, errors.New("unknown sku")
	}
	return []ProductSnapshot{r.product}, nil
}
