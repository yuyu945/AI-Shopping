package user

import "fmt"

type User struct {
	ID           uint64
	Email        string
	PasswordHash string
}

type Profile struct {
	UserID         uint64
	PreferenceJSON []byte
	BudgetMin      *string
	BudgetMax      *string
	Version        uint64
}

type Address struct {
	ID uint64
	AddressInput
}

type AddressInput struct {
	ReceiverName, ReceiverPhone, Province, City, District, Detail string
	IsDefault                                                     bool
}

type ProfileUpdate struct {
	PreferenceJSON []byte
	BudgetMin      *string
	BudgetMax      *string
}

type NotFoundError struct{ Resource string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("%s not found", e.Resource) }

type DuplicateEmailError struct{}

func (DuplicateEmailError) Error() string { return "email already exists" }
