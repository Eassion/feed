package userrepo

import (
	"context"
	"errors"

	"feed/internal/model"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

var ErrRepositoryUnavailable = errors.New("user repository is unavailable")

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	user := &model.User{}
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(user).Error; err != nil {
		return nil, err
	}

	return user, nil
}

func (r *Repository) FindByID(ctx context.Context, userID int64) (*model.User, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	user := &model.User{}
	if err := r.db.WithContext(ctx).First(user, userID).Error; err != nil {
		return nil, err
	}

	return user, nil
}

func (r *Repository) Create(ctx context.Context, user *model.User) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}

	return r.db.WithContext(ctx).Create(user).Error
}

func (r *Repository) UpdateProfile(ctx context.Context, userID int64, updates map[string]any) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}
	if len(updates) == 0 {
		return nil
	}

	tx := r.db.WithContext(ctx).
		Model(&model.User{}).
		Where("id = ?", userID).
		Updates(updates)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}

	return nil
}

func IsDuplicateUsername(err error) bool {
	var mysqlErr *driver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}

	return false
}
