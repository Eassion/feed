package countrepo

import (
	"context"
	"errors"

	"feed/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrRepositoryUnavailable = errors.New("count repository is unavailable")

type Repository struct {
	db *gorm.DB
}

type Mutation struct {
	ContentID              int64
	LikeDelta              int64
	FavoriteDelta          int64
	CommentDelta           int64
	UserID                 int64
	LikesReceivedDelta     int64
	FavoritesReceivedDelta int64
	FollowersDelta         int64
	FollowingDelta         int64
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) EnsureContentCounter(ctx context.Context, contentID int64) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}
	if contentID <= 0 {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.ensureCountValue(tx, model.CountBizTypeLike, model.CountTargetTypeContent, contentID, 0); err != nil {
			return err
		}
		if err := r.ensureCountValue(tx, model.CountBizTypeFavorite, model.CountTargetTypeContent, contentID, 0); err != nil {
			return err
		}
		return r.ensureCountValue(tx, model.CountBizTypeComment, model.CountTargetTypeContent, contentID, 0)
	})
}

func (r *Repository) EnsureUserCounter(ctx context.Context, userID int64) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}
	if userID <= 0 {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.ensureCountValue(tx, model.CountBizTypeLike, model.CountTargetTypeUser, userID, userID); err != nil {
			return err
		}
		if err := r.ensureCountValue(tx, model.CountBizTypeFavorite, model.CountTargetTypeUser, userID, userID); err != nil {
			return err
		}
		if err := r.ensureCountValue(tx, model.CountBizTypeFollowed, model.CountTargetTypeUser, userID, userID); err != nil {
			return err
		}
		return r.ensureCountValue(tx, model.CountBizTypeFollowing, model.CountTargetTypeUser, userID, userID)
	})
}

func (r *Repository) GetContentCounter(ctx context.Context, contentID int64) (*model.ContentCount, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	rows, err := r.listCountValues(ctx, []int32{model.CountBizTypeLike, model.CountBizTypeFavorite, model.CountBizTypeComment}, model.CountTargetTypeContent, []int64{contentID})
	if err != nil {
		return nil, err
	}

	counter := model.ContentCount{ContentID: contentID}
	for _, row := range rows {
		switch row.BizType {
		case model.CountBizTypeLike:
			counter.LikeCount = row.Value
		case model.CountBizTypeFavorite:
			counter.FavoriteCount = row.Value
		case model.CountBizTypeComment:
			counter.CommentCount = row.Value
		}
	}
	return &counter, nil
}

func (r *Repository) GetUserCounter(ctx context.Context, userID int64) (*model.UserCount, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	rows, err := r.listCountValues(ctx, []int32{model.CountBizTypeLike, model.CountBizTypeFavorite, model.CountBizTypeFollowed, model.CountBizTypeFollowing}, model.CountTargetTypeUser, []int64{userID})
	if err != nil {
		return nil, err
	}

	counter := model.UserCount{UserID: userID}
	for _, row := range rows {
		switch row.BizType {
		case model.CountBizTypeLike:
			counter.TotalLikesReceived = row.Value
		case model.CountBizTypeFavorite:
			counter.TotalFavoritesReceived = row.Value
		case model.CountBizTypeFollowed:
			counter.FollowersCount = row.Value
		case model.CountBizTypeFollowing:
			counter.FollowingCount = row.Value
		}
	}
	return &counter, nil
}

func (r *Repository) BatchGetContentCounterMap(ctx context.Context, contentIDs []int64) (map[int64]model.ContentCount, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	result := make(map[int64]model.ContentCount, len(contentIDs))
	if len(contentIDs) == 0 {
		return result, nil
	}
	for _, contentID := range contentIDs {
		result[contentID] = model.ContentCount{ContentID: contentID}
	}

	rows, err := r.listCountValues(ctx, []int32{model.CountBizTypeLike, model.CountBizTypeFavorite, model.CountBizTypeComment}, model.CountTargetTypeContent, contentIDs)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		counter := result[row.TargetID]
		switch row.BizType {
		case model.CountBizTypeLike:
			counter.LikeCount = row.Value
		case model.CountBizTypeFavorite:
			counter.FavoriteCount = row.Value
		case model.CountBizTypeComment:
			counter.CommentCount = row.Value
		}
		result[row.TargetID] = counter
	}

	return result, nil
}

//在一个事务中同时更新计数表和主表的冗余计数字段，确保数据一致性
//有以content_id为对象的更新   也有以user_id为对象的更新，甚至可能两者都有
func (r *Repository) ApplyMutation(ctx context.Context, mutation Mutation) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if mutation.ContentID > 0 {
			//确保数据库已经存在一条记录了
			if err := r.ensureCountValue(tx, model.CountBizTypeLike, model.CountTargetTypeContent, mutation.ContentID, 0); err != nil {
				return err
			}
			if err := r.ensureCountValue(tx, model.CountBizTypeFavorite, model.CountTargetTypeContent, mutation.ContentID, 0); err != nil {
				return err
			}
			if err := r.ensureCountValue(tx, model.CountBizTypeComment, model.CountTargetTypeContent, mutation.ContentID, 0); err != nil {
				return err
			}
			if mutation.LikeDelta != 0 {
				//更新
				if err := r.addCountValue(tx, model.CountBizTypeLike, model.CountTargetTypeContent, mutation.ContentID, mutation.LikeDelta); err != nil {
					return err
				}
			}
			if mutation.FavoriteDelta != 0 {
				if err := r.addCountValue(tx, model.CountBizTypeFavorite, model.CountTargetTypeContent, mutation.ContentID, mutation.FavoriteDelta); err != nil {
					return err
				}
			}
			if mutation.CommentDelta != 0 {
				if err := r.addCountValue(tx, model.CountBizTypeComment, model.CountTargetTypeContent, mutation.ContentID, mutation.CommentDelta); err != nil {
					return err
				}
			}

			//更新主表里面的冗余计数字段
			updates := map[string]any{}
			if mutation.LikeDelta != 0 {
				updates["like_count"] = gorm.Expr("like_count + ?", mutation.LikeDelta)
			}
			if mutation.FavoriteDelta != 0 {
				updates["favorite_count"] = gorm.Expr("favorite_count + ?", mutation.FavoriteDelta)
			}
			if mutation.CommentDelta != 0 {
				updates["comment_count"] = gorm.Expr("comment_count + ?", mutation.CommentDelta)
			}
			if len(updates) > 0 {
				if err := tx.Table("ran_feed_content").Where("id = ?", mutation.ContentID).UpdateColumns(updates).Error; err != nil {
					return err
				}
			}
		}

		//如果mutation里带了user_id，说明这个变更还会影响用户维度的统计数据，比如like事件既会增加content_id的like_count，也会增加content_user_id的total_likes_received
		if mutation.UserID > 0 {
			if err := r.ensureCountValue(tx, model.CountBizTypeLike, model.CountTargetTypeUser, mutation.UserID, mutation.UserID); err != nil {
				return err
			}
			if err := r.ensureCountValue(tx, model.CountBizTypeFavorite, model.CountTargetTypeUser, mutation.UserID, mutation.UserID); err != nil {
				return err
			}
			if err := r.ensureCountValue(tx, model.CountBizTypeFollowed, model.CountTargetTypeUser, mutation.UserID, mutation.UserID); err != nil {
				return err
			}
			if err := r.ensureCountValue(tx, model.CountBizTypeFollowing, model.CountTargetTypeUser, mutation.UserID, mutation.UserID); err != nil {
				return err
			}
			if mutation.LikesReceivedDelta != 0 {
				if err := r.addCountValue(tx, model.CountBizTypeLike, model.CountTargetTypeUser, mutation.UserID, mutation.LikesReceivedDelta); err != nil {
					return err
				}
			}
			if mutation.FavoritesReceivedDelta != 0 {
				if err := r.addCountValue(tx, model.CountBizTypeFavorite, model.CountTargetTypeUser, mutation.UserID, mutation.FavoritesReceivedDelta); err != nil {
					return err
				}
			}
			if mutation.FollowersDelta != 0 {
				if err := r.addCountValue(tx, model.CountBizTypeFollowed, model.CountTargetTypeUser, mutation.UserID, mutation.FollowersDelta); err != nil {
					return err
				}
			}
			if mutation.FollowingDelta != 0 {
				if err := r.addCountValue(tx, model.CountBizTypeFollowing, model.CountTargetTypeUser, mutation.UserID, mutation.FollowingDelta); err != nil {
					return err
				}
			}
		}

		return nil
	})
}

//确保数据库里存在一条value为0的记录
func (r *Repository) ensureCountValue(tx *gorm.DB, bizType, targetType int32, targetID, ownerID int64) error {
	row := model.CountValue{
		BizType:    bizType,
		TargetType: targetType,
		TargetID:   targetID,
		OwnerID:    ownerID,
		Value:      0,
		Version:    1,
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

func (r *Repository) addCountValue(tx *gorm.DB, bizType, targetType int32, targetID, delta int64) error {
	return tx.Model(&model.CountValue{}).
		Where("biz_type = ? AND target_type = ? AND target_id = ?", bizType, targetType, targetID).
		Updates(map[string]any{
			"value":   gorm.Expr("value + ?", delta),
			"version": gorm.Expr("version + 1"),
		}).Error
}

func (r *Repository) listCountValues(ctx context.Context, bizTypes []int32, targetType int32, targetIDs []int64) ([]model.CountValue, error) {
	rows := make([]model.CountValue, 0)
	if len(targetIDs) == 0 {
		return rows, nil
	}
	if err := r.db.WithContext(ctx).
		Where("biz_type IN ? AND target_type = ? AND target_id IN ?", bizTypes, targetType, targetIDs).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
