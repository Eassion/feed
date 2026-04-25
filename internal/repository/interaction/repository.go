package interactionrepo

import (
	"context"
	"errors"
	"time"

	"feed/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrRepositoryUnavailable = errors.New("interaction repository is unavailable")
	ErrCommentNotFound       = errors.New("comment not found")
)

type Repository struct {
	db *gorm.DB
}

type CommentDeleteResult struct {
	DeletedComment    *model.Comment
	Tombstoned        bool
	PhysicallyRemoved []model.Comment
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) UpsertLike(ctx context.Context, like *model.Like) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}
	if like == nil {
		return nil
	}
	if like.Version == 0 {
		like.Version = 1
	}

	//幂等落库
	//不存在就插入，已存在就更新状态和版本号等信息
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "content_id"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"status":          like.Status,
			"content_user_id": like.ContentUserID,
			"is_deleted":      false,
			"updated_by":      like.UpdatedBy,
			"updated_at":      time.Now(),
			"version":         gorm.Expr("version + 1"),
		}),
	}).Create(like).Error
}

func (r *Repository) EnsureLike(ctx context.Context, userID, contentID, contentUserID int64) (bool, error) {
	if r.db == nil {
		return false, ErrRepositoryUnavailable
	}

	like := &model.Like{}
	err := r.db.WithContext(ctx).Where("user_id = ? AND content_id = ?", userID, contentID).First(like).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		like = &model.Like{
			UserID:        userID,
			ContentID:     contentID,
			ContentUserID: contentUserID,
			Status:        model.InteractionStatusActive,
			Version:       1,
			CreatedBy:     userID,
			UpdatedBy:     userID,
		}
		return true, r.db.WithContext(ctx).Create(like).Error
	}
	if err != nil {
		return false, err
	}
	if like.Status == model.InteractionStatusActive && !like.IsDeleted {
		return false, nil
	}

	tx := r.db.WithContext(ctx).Model(&model.Like{}).Where("id = ?", like.ID).Updates(map[string]any{
		"status":          model.InteractionStatusActive,
		"is_deleted":      false,
		"content_user_id": contentUserID,
		"updated_by":      userID,
		"updated_at":      time.Now(),
		"version":         gorm.Expr("version + 1"),
	})
	return tx.RowsAffected > 0, tx.Error
}

func (r *Repository) RemoveLike(ctx context.Context, userID, contentID int64) (bool, error) {
	if r.db == nil {
		return false, ErrRepositoryUnavailable
	}

	tx := r.db.WithContext(ctx).Model(&model.Like{}).
		Where("user_id = ? AND content_id = ? AND is_deleted = ? AND status = ?", userID, contentID, false, model.InteractionStatusActive).
		Updates(map[string]any{
			"status":     model.InteractionStatusCanceled,
			"is_deleted": true,
			"updated_by": userID,
			"updated_at": time.Now(),
			"version":    gorm.Expr("version + 1"),
		})
	return tx.RowsAffected > 0, tx.Error
}

func (r *Repository) EnsureFavorite(ctx context.Context, userID, contentID, contentUserID int64) (int64, bool, error) {
	if r.db == nil {
		return 0, false, ErrRepositoryUnavailable
	}

	favorite := &model.Favorite{}
	err := r.db.WithContext(ctx).Where("user_id = ? AND content_id = ?", userID, contentID).First(favorite).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		favorite = &model.Favorite{
			UserID:        userID,
			ContentID:     contentID,
			ContentUserID: contentUserID,
			Status:        model.InteractionStatusActive,
			CreatedBy:     userID,
			UpdatedBy:     userID,
		}
		if err := r.db.WithContext(ctx).Create(favorite).Error; err != nil {
			return 0, false, err
		}
		return favorite.ID, true, nil
	}
	if err != nil {
		return 0, false, err
	}
	if favorite.Status == model.InteractionStatusActive {
		return favorite.ID, false, nil
	}

	tx := r.db.WithContext(ctx).Model(&model.Favorite{}).Where("id = ?", favorite.ID).Updates(map[string]any{
		"status":          model.InteractionStatusActive,
		"content_user_id": contentUserID,
		"updated_by":      userID,
		"updated_at":      time.Now(),
	})
	return favorite.ID, tx.RowsAffected > 0, tx.Error
}

func (r *Repository) RemoveFavorite(ctx context.Context, userID, contentID int64) (bool, error) {
	if r.db == nil {
		return false, ErrRepositoryUnavailable
	}

	tx := r.db.WithContext(ctx).Model(&model.Favorite{}).
		Where("user_id = ? AND content_id = ? AND status = ?", userID, contentID, model.InteractionStatusActive).
		Updates(map[string]any{
			"status":     model.InteractionStatusCanceled,
			"updated_by": userID,
			"updated_at": time.Now(),
		})
	return tx.RowsAffected > 0, tx.Error
}

func (r *Repository) CreateComment(ctx context.Context, comment *model.Comment) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}
	if comment.Status == 0 {
		comment.Status = model.InteractionStatusActive
	}
	if comment.Version == 0 {
		comment.Version = 1
	}
	comment.CreatedBy = comment.UserID
	comment.UpdatedBy = comment.UserID

	return r.db.WithContext(ctx).Create(comment).Error
}

func (r *Repository) GetActiveComment(ctx context.Context, commentID int64) (*model.Comment, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	comment := &model.Comment{}
	if err := r.db.WithContext(ctx).
		Where("id = ? AND is_deleted = ? AND status = ?", commentID, false, model.InteractionStatusActive).
		First(comment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCommentNotFound
		}
		return nil, err
	}

	return comment, nil
}

func (r *Repository) ListActiveTopLevelComments(ctx context.Context, contentID int64) ([]model.Comment, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	var comments []model.Comment
	if err := r.db.WithContext(ctx).
		Where("content_id = ? AND parent_id = ?", contentID, 0).
		Order("id DESC").
		Find(&comments).Error; err != nil {
		return nil, err
	}
	return comments, nil
}

func (r *Repository) ListActiveReplies(ctx context.Context, rootID int64) ([]model.Comment, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	var comments []model.Comment
	if err := r.db.WithContext(ctx).
		Where("root_id = ? AND parent_id <> ?", rootID, 0).
		Order("id DESC").
		Find(&comments).Error; err != nil {
		return nil, err
	}
	return comments, nil
}

func (r *Repository) ListActiveCommentsByIDs(ctx context.Context, commentIDs []int64) ([]model.Comment, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}
	if len(commentIDs) == 0 {
		return []model.Comment{}, nil
	}

	var comments []model.Comment
	if err := r.db.WithContext(ctx).
		Where("id IN ?", commentIDs).
		Find(&comments).Error; err != nil {
		return nil, err
	}
	return comments, nil
}

func (r *Repository) CountRepliesByRootIDs(ctx context.Context, rootIDs []int64) (map[int64]int64, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}
	result := make(map[int64]int64, len(rootIDs))
	if len(rootIDs) == 0 {
		return result, nil
	}

	type replyCount struct {
		RootID int64 `gorm:"column:root_id"`
		Count  int64 `gorm:"column:cnt"`
	}

	var rows []replyCount
	if err := r.db.WithContext(ctx).
		Model(&model.Comment{}).
		Select("root_id, COUNT(*) AS cnt").
		Where("root_id IN ? AND parent_id <> ?", rootIDs, 0).
		Group("root_id").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		result[row.RootID] = row.Count
	}
	return result, nil
}

func (r *Repository) ListActiveCommentsByContent(ctx context.Context, contentID int64) ([]model.Comment, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	var comments []model.Comment
	if err := r.db.WithContext(ctx).
		Where("content_id = ?", contentID).
		Find(&comments).Error; err != nil {
		return nil, err
	}
	return comments, nil
}

func (r *Repository) DeleteCommentByAuthor(ctx context.Context, commentID, userID int64) (*CommentDeleteResult, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	result := &CommentDeleteResult{}
	//查找要删除的评论记录
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		comment := &model.Comment{}
		if err := tx.
			Where("id = ? AND user_id = ? AND is_deleted = ? AND status = ?", commentID, userID, false, model.InteractionStatusActive).
			First(comment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCommentNotFound
			}
			return err
		}

		commentCopy := *comment
		//保存这条记录返回时使用
		result.DeletedComment = &commentCopy

		//是否还存在引用这条评论的其他评论
		hasRefs, err := r.hasReferencesTx(tx, comment.ID)
		if err != nil {
			return err
		}
		if hasRefs {
			//逻辑删除
			if err := tx.Model(&model.Comment{}).Where("id = ?", comment.ID).Updates(map[string]any{
				"status":     model.InteractionStatusCanceled,
				"is_deleted": true,
				"updated_by": userID,
				"updated_at": time.Now(),
				"version":    gorm.Expr("version + 1"),
			}).Error; err != nil {
				return err
			}
			result.Tombstoned = true
			result.DeletedComment.Status = model.InteractionStatusCanceled
			result.DeletedComment.IsDeleted = true
			result.DeletedComment.UpdatedBy = userID
			result.DeletedComment.UpdatedAt = time.Now()
			return nil
		}

		//物理删除
		if err := tx.Delete(&model.Comment{}, comment.ID).Error; err != nil {
			return err
		}
		result.PhysicallyRemoved = append(result.PhysicallyRemoved, commentCopy)

		//递归向上删除，统计删除了哪些评论
		removedAncestors, err := r.cleanupDeletedAncestorsTx(tx, comment.ParentID)
		if err != nil {
			return err
		}
		result.PhysicallyRemoved = append(result.PhysicallyRemoved, removedAncestors...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (r *Repository) hasReferencesTx(tx *gorm.DB, commentID int64) (bool, error) {
	if commentID <= 0 {
		return false, nil
	}

	var count int64
	if err := tx.Model(&model.Comment{}).
		Where("(parent_id = ? OR root_id = ?) AND is_deleted = ?", commentID, commentID, false).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) cleanupDeletedAncestorsTx(tx *gorm.DB, parentID int64) ([]model.Comment, error) {
	removed := make([]model.Comment, 0)
	currentID := parentID
	for currentID > 0 {
		comment := &model.Comment{}
		if err := tx.Where("id = ?", currentID).First(comment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return removed, nil
			}
			return nil, err
		}
		if !comment.IsDeleted {
			return removed, nil
		}

		hasRefs, err := r.hasReferencesTx(tx, comment.ID)
		if err != nil {
			return nil, err
		}
		if hasRefs {
			return removed, nil
		}

		commentCopy := *comment
		if err := tx.Delete(&model.Comment{}, comment.ID).Error; err != nil {
			return nil, err
		}
		removed = append(removed, commentCopy)
		currentID = comment.ParentID
	}

	return removed, nil
}

func (r *Repository) EnsureFollow(ctx context.Context, followerID, followeeID int64) (bool, error) {
	if r.db == nil {
		return false, ErrRepositoryUnavailable
	}

	follow := &model.Follow{}
	err := r.db.WithContext(ctx).Where("user_id = ? AND follow_user_id = ?", followerID, followeeID).First(follow).Error
	//还没有关注关系时直接创建
	if errors.Is(err, gorm.ErrRecordNotFound) {
		follow = &model.Follow{
			UserID:       followerID,
			FollowUserID: followeeID,
			Status:       model.InteractionStatusActive,
			Version:      1,
			CreatedBy:    followerID,
			UpdatedBy:    followerID,
		}
		return true, r.db.WithContext(ctx).Create(follow).Error
	}
	if err != nil {
		return false, err
	}
	//已经是关注关系了，并且没有被删除，无需修改
	if follow.Status == model.InteractionStatusActive && !follow.IsDeleted {
		return false, nil
	}

	//之前存在过关注关系了，但是被取消了或者被删除了，现在要修改成有效的关注关系了
	tx := r.db.WithContext(ctx).Model(&model.Follow{}).Where("id = ?", follow.ID).Updates(map[string]any{
		"status":     model.InteractionStatusActive,
		"is_deleted": false,
		"updated_by": followerID,
		"updated_at": time.Now(),
		"version":    gorm.Expr("version + 1"),
	})
	return tx.RowsAffected > 0, tx.Error
}

func (r *Repository) RemoveFollow(ctx context.Context, followerID, followeeID int64) (bool, error) {
	if r.db == nil {
		return false, ErrRepositoryUnavailable
	}

	tx := r.db.WithContext(ctx).Model(&model.Follow{}).
		Where("user_id = ? AND follow_user_id = ? AND is_deleted = ? AND status = ?", followerID, followeeID, false, model.InteractionStatusActive).
		Updates(map[string]any{
			"status":     model.InteractionStatusCanceled,
			"is_deleted": true,
			"updated_by": followerID,
			"updated_at": time.Now(),
			"version":    gorm.Expr("version + 1"),
		})
	return tx.RowsAffected > 0, tx.Error
}

func (r *Repository) BatchGetLikeMap(ctx context.Context, userID int64, contentIDs []int64) (map[int64]bool, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}
	result := make(map[int64]bool, len(contentIDs))
	if len(contentIDs) == 0 {
		return result, nil
	}

	var likes []model.Like
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND content_id IN ? AND is_deleted = ? AND status = ?", userID, contentIDs, false, model.InteractionStatusActive).
		Find(&likes).Error; err != nil {
		return nil, err
	}

	for _, like := range likes {
		result[like.ContentID] = true
	}
	return result, nil
}

func (r *Repository) BatchGetFavoriteMap(ctx context.Context, userID int64, contentIDs []int64) (map[int64]bool, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}
	result := make(map[int64]bool, len(contentIDs))
	if len(contentIDs) == 0 {
		return result, nil
	}

	var favorites []model.Favorite
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND content_id IN ? AND status = ?", userID, contentIDs, model.InteractionStatusActive).
		Find(&favorites).Error; err != nil {
		return nil, err
	}

	for _, favorite := range favorites {
		result[favorite.ContentID] = true
	}
	return result, nil
}

func (r *Repository) IsFollowing(ctx context.Context, followerID, followeeID int64) (bool, error) {
	if r.db == nil {
		return false, ErrRepositoryUnavailable
	}

	var count int64
	if err := r.db.WithContext(ctx).
		Model(&model.Follow{}).
		Where("user_id = ? AND follow_user_id = ? AND is_deleted = ? AND status = ?", followerID, followeeID, false, model.InteractionStatusActive).
		Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

func (r *Repository) ListFollowerIDs(ctx context.Context, followeeID int64) ([]int64, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	var follows []model.Follow
	if err := r.db.WithContext(ctx).
		Where("follow_user_id = ? AND is_deleted = ? AND status = ?", followeeID, false, model.InteractionStatusActive).
		Find(&follows).Error; err != nil {
		return nil, err
	}

	result := make([]int64, 0, len(follows))
	for _, follow := range follows {
		result = append(result, follow.UserID)
	}
	return result, nil
}

func (r *Repository) ListFolloweeIDs(ctx context.Context, followerID int64) ([]int64, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	var follows []model.Follow
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND is_deleted = ? AND status = ?", followerID, false, model.InteractionStatusActive).
		Find(&follows).Error; err != nil {
		return nil, err
	}

	result := make([]int64, 0, len(follows))
	for _, follow := range follows {
		result = append(result, follow.FollowUserID)
	}
	return result, nil
}

func (r *Repository) ListFollowerUserIDsForRebuild(ctx context.Context, afterUserID int64, limit int) ([]int64, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}
	if limit <= 0 {
		limit = 200
	}

	userIDs := make([]int64, 0, limit)
	query := r.db.WithContext(ctx).
		Model(&model.Follow{}).
		Distinct("user_id").
		Where("status = ? AND is_deleted = ?", model.InteractionStatusActive, false)
	if afterUserID > 0 {
		query = query.Where("user_id > ?", afterUserID)
	}
	if err := query.Order("user_id ASC").Limit(limit).Pluck("user_id", &userIDs).Error; err != nil {
		return nil, err
	}
	return userIDs, nil
}

func (r *Repository) ListFavoriteEntries(ctx context.Context, userID int64, limit int64) ([]model.Favorite, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}
	if userID <= 0 || limit <= 0 {
		return []model.Favorite{}, nil
	}

	var favorites []model.Favorite
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND status = ?", userID, model.InteractionStatusActive).
		Order("id DESC").
		Limit(int(limit)).
		Find(&favorites).Error; err != nil {
		return nil, err
	}

	return favorites, nil
}
