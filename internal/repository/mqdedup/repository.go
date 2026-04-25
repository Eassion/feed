package mqdeduprepo

import (
	"context"
	"errors"

	"feed/internal/model"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrRepositoryUnavailable = errors.New("mq dedup repository is unavailable")

type Repository struct {
	db       *gorm.DB
	consumer string
}

func New(db *gorm.DB, consumer string) *Repository {
	return &Repository{
		db:       db,
		consumer: consumer,
	}
}

func (r *Repository) MarkOnce(ctx context.Context, eventID string) (bool, error) {
	if r.db == nil {
		return false, ErrRepositoryUnavailable
	}
	if eventID == "" {
		return true, nil
	}

	row := &model.MQConsumeDedup{
		Consumer: r.consumer,
		EventID:  eventID,
	}
	tx := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	return tx.RowsAffected > 0, tx.Error
}

func IsDuplicateEvent(err error) bool {
	var mysqlErr *driver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
