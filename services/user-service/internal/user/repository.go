package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) CreateUserWithProfile(ctx context.Context, email, passwordHash string) (User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, fmt.Errorf("begin create user: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, "INSERT INTO users (email, password_hash) VALUES (?, ?)", email, passwordHash)
	if err != nil {
		return User{}, duplicateEmail(err)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return User{}, fmt.Errorf("create user id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO user_profiles (user_id) VALUES (?)", uint64(id)); err != nil {
		return User{}, fmt.Errorf("create user profile: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("commit create user: %w", err)
	}
	return User{ID: uint64(id), Email: email, PasswordHash: passwordHash}, nil
}

func (r *Repository) FindUserByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := r.db.QueryRowContext(ctx, "SELECT id, email, password_hash FROM users WHERE email = ? AND status = 'ACTIVE'", email).Scan(&user.ID, &user.Email, &user.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, &NotFoundError{Resource: "user"}
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	return user, nil
}

func (r *Repository) GetProfile(ctx context.Context, userID uint64) (Profile, error) {
	var profile Profile
	var min, max sql.NullString
	err := r.db.QueryRowContext(ctx, "SELECT preference_json, budget_min, budget_max, profile_version FROM user_profiles WHERE user_id = ?", userID).Scan(&profile.PreferenceJSON, &min, &max, &profile.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, &NotFoundError{Resource: "profile"}
	}
	if err != nil {
		return Profile{}, fmt.Errorf("get profile: %w", err)
	}
	profile.UserID = userID
	if min.Valid {
		profile.BudgetMin = &min.String
	}
	if max.Valid {
		profile.BudgetMax = &max.String
	}
	return profile, nil
}

func (r *Repository) UpdateProfile(ctx context.Context, userID uint64, update ProfileUpdate) (Profile, error) {
	var min, max any
	if update.BudgetMin != nil {
		min = *update.BudgetMin
	}
	if update.BudgetMax != nil {
		max = *update.BudgetMax
	}
	result, err := r.db.ExecContext(ctx, "UPDATE user_profiles SET preference_json = ?, budget_min = ?, budget_max = ?, profile_version = profile_version + 1 WHERE user_id = ?", update.PreferenceJSON, min, max, userID)
	if err != nil {
		return Profile{}, fmt.Errorf("update profile: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Profile{}, err
	}
	if affected == 0 {
		return Profile{}, &NotFoundError{Resource: "profile"}
	}
	return r.GetProfile(ctx, userID)
}

func (r *Repository) CreateAddress(ctx context.Context, userID uint64, input AddressInput) (Address, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Address{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, "INSERT INTO user_addresses (user_id, receiver_name, receiver_phone, province, city, district, detail, is_default) VALUES (?, ?, ?, ?, ?, ?, ?, FALSE)", userID, input.ReceiverName, input.ReceiverPhone, input.Province, input.City, input.District, input.Detail)
	if err != nil {
		return Address{}, fmt.Errorf("create address: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return Address{}, fmt.Errorf("create address id: %w", err)
	}
	var count uint64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_addresses WHERE user_id = ?", userID).Scan(&count); err != nil {
		return Address{}, fmt.Errorf("count addresses: %w", err)
	}
	defaulted := input.IsDefault || count == 1
	if input.IsDefault && count > 1 {
		if _, err := tx.ExecContext(ctx, "UPDATE user_addresses SET is_default = FALSE WHERE user_id = ?", userID); err != nil {
			return Address{}, fmt.Errorf("clear default: %w", err)
		}
	}
	if defaulted {
		if _, err := tx.ExecContext(ctx, "UPDATE user_addresses SET is_default = TRUE WHERE id = ? AND user_id = ?", uint64(id), userID); err != nil {
			return Address{}, fmt.Errorf("set default: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Address{}, err
	}
	input.IsDefault = defaulted
	return Address{ID: uint64(id), AddressInput: input}, nil
}

func (r *Repository) UpdateAddress(ctx context.Context, userID, addressID uint64, input AddressInput) (Address, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Address{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, "UPDATE user_addresses SET receiver_name = ?, receiver_phone = ?, province = ?, city = ?, district = ?, detail = ?, is_default = ? WHERE id = ? AND user_id = ?", input.ReceiverName, input.ReceiverPhone, input.Province, input.City, input.District, input.Detail, input.IsDefault, addressID, userID)
	if err != nil {
		return Address{}, fmt.Errorf("update address: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Address{}, err
	}
	if affected == 0 {
		return Address{}, &NotFoundError{Resource: "address"}
	}
	if input.IsDefault {
		if _, err := tx.ExecContext(ctx, "UPDATE user_addresses SET is_default = FALSE WHERE user_id = ? AND id <> ?", userID, addressID); err != nil {
			return Address{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Address{}, err
	}
	return Address{ID: addressID, AddressInput: input}, nil
}

func (r *Repository) DeleteAddress(ctx context.Context, userID, addressID uint64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var isDefault bool
	if err := tx.QueryRowContext(ctx, "SELECT is_default FROM user_addresses WHERE id = ? AND user_id = ?", addressID, userID).Scan(&isDefault); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &NotFoundError{Resource: "address"}
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_addresses WHERE id = ? AND user_id = ?", addressID, userID); err != nil {
		return err
	}
	if isDefault {
		if _, err := tx.ExecContext(ctx, "UPDATE user_addresses SET is_default = TRUE WHERE user_id = ? ORDER BY id ASC LIMIT 1", userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func duplicateEmail(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return DuplicateEmailError{}
	}
	return fmt.Errorf("create user: %w", err)
}
