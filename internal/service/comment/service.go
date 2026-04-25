package commentsvc

import (
	"context"
	"errors"

	"feed/internal/cache"
	"feed/internal/model"
	interactionrepo "feed/internal/repository/interaction"
)

var ErrInvalidCommentQuery = errors.New("invalid comment query")

type Service struct {
	repo     *interactionrepo.Repository
	cache    *cache.CommentStore
	users    CommentUserProvider
	delegate CommentSyncDelegate
}

type CommentUserProvider interface {
	BatchGetUserMap(ctx context.Context, userIDs []int64) (map[int64]model.UserSummary, error)
}

type CommentSyncDelegate interface {
	HandleCommentChanged(ctx context.Context, comment *model.Comment, delta int64)
}

func New(repo *interactionrepo.Repository, cacheStore *cache.CommentStore, users CommentUserProvider) *Service {
	return &Service{
		repo:  repo,
		cache: cacheStore,
		users: users,
	}
}

func (s *Service) SetDelegate(delegate CommentSyncDelegate) {
	s.delegate = delegate
}

func (s *Service) ListContentComments(ctx context.Context, contentID int64, cursor string, limit int) (*model.CommentPage, error) {
	if contentID <= 0 {
		return nil, ErrInvalidCommentQuery
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	exists, err := s.cache.ContentIndexExists(ctx, contentID)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := s.rebuildContentIndex(ctx, contentID); err != nil {
			return nil, err
		}
	}

	commentIDs, nextCursor, hasMore, err := s.cache.ListContentIndex(ctx, contentID, cursor, limit)
	if err != nil {
		return nil, err
	}

	items, err := s.loadCommentItems(ctx, commentIDs, true)
	if err != nil {
		return nil, err
	}

	return &model.CommentPage{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (s *Service) ListReplies(ctx context.Context, rootID int64, cursor string, limit int) (*model.CommentPage, error) {
	if rootID <= 0 {
		return nil, ErrInvalidCommentQuery
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	exists, err := s.cache.ReplyIndexExists(ctx, rootID)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := s.rebuildReplyIndex(ctx, rootID); err != nil {
			return nil, err
		}
	}

	commentIDs, nextCursor, hasMore, err := s.cache.ListReplyIndex(ctx, rootID, cursor, limit)
	if err != nil {
		return nil, err
	}

	items, err := s.loadCommentItems(ctx, commentIDs, false)
	if err != nil {
		return nil, err
	}

	return &model.CommentPage{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (s *Service) HandleCommentCreated(ctx context.Context, comment *model.Comment) {
	if comment == nil {
		return
	}

	//处理评论相关的缓存和索引更新
	s.handleCommentCreated(ctx, comment)
	if s.delegate != nil {
		//新增评论和删除评论都可能影响内容的评论数和相关回复的评论数，delta为1表示增加，-1表示减少
		s.delegate.HandleCommentChanged(ctx, comment, 1)
	}
}

func (s *Service) HandleCommentDeleted(ctx context.Context, result *interactionrepo.CommentDeleteResult) {
	if result == nil || result.DeletedComment == nil {
		return
	}

	//处理评论删除的缓存和索引更新
	s.handleCommentDeleted(ctx, result)
	if s.delegate != nil {
		//删除评论可能影响内容的评论数和相关回复的评论数，delta为-1表示减少
		s.delegate.HandleCommentChanged(ctx, result.DeletedComment, -1)
	}
}

func (s *Service) HandleContentDeleted(ctx context.Context, contentID int64) {
	if s == nil || s.repo == nil || s.cache == nil || contentID <= 0 {
		return
	}

	//查询该内容下的所有评论，准备批量删除缓存和索引
	comments, err := s.repo.ListActiveCommentsByContent(ctx, contentID)
	if err != nil {
		return
	}

	commentIDs := make([]int64, 0, len(comments))
	rootIDs := make(map[int64]struct{})
	for _, comment := range comments {
		commentIDs = append(commentIDs, comment.ID)
		if comment.ParentID == 0 {
			rootIDs[comment.ID] = struct{}{}
			continue
		}
		if comment.RootID > 0 {
			rootIDs[comment.RootID] = struct{}{}
		}
	}

	//批量删除评论对象缓存
	_ = s.cache.DeleteCommentObjects(ctx, commentIDs)
	//失效内容评论索引
	_ = s.cache.InvalidateContentIndex(ctx, contentID)
	//失效相关回复索引
	for rootID := range rootIDs {
		_ = s.cache.InvalidateReplyIndex(ctx, rootID)
	}
}

//处理评论创建的缓存和索引更新逻辑
func (s *Service) handleCommentCreated(ctx context.Context, comment *model.Comment) {
	if s == nil || s.cache == nil || comment == nil {
		return
	}

	//组装成返回给前端的commentItem
	//有示例
	items, err := s.buildCommentItems(ctx, []model.Comment{*comment}, comment.ParentID == 0)
	if err == nil && len(items) > 0 {
		_ = s.cache.SetCommentObjects(ctx, items)
	}

	if comment.ParentID == 0 {
		_ = s.cache.AddToContentIndex(ctx, comment.ContentID, comment.ID)
		return
	}

	_ = s.cache.DeleteCommentObject(ctx, comment.ParentID)
	//如果是回复评论，还需要失效根评论对象和相关回复索引
	if comment.RootID > 0 {
		//是因为replayCount的变化导致根评论对象需要更新，所以直接删除缓存，让下次访问时重建
		_ = s.cache.DeleteCommentObject(ctx, comment.RootID)
		//删除：comment:idx:root:101
    // comment:idx:root:init:101
    // comment:idx:root:lock:101
		_ = s.cache.InvalidateReplyIndex(ctx, comment.RootID)
	}
}

func (s *Service) handleCommentDeleted(ctx context.Context, result *interactionrepo.CommentDeleteResult) {
	if s == nil || s.cache == nil || result == nil || result.DeletedComment == nil {
		return
	}

	comment := result.DeletedComment
	//逻辑删除  主要是修改评论内容为：comment deleted，其他字段不变，保持评论对象存在，这样可以避免删除后导致的索引错乱和数据不一致问题
	if result.Tombstoned {
		items, err := s.buildCommentItems(ctx, []model.Comment{*comment}, comment.ParentID == 0)
		if err == nil && len(items) > 0 {
			_ = s.cache.SetCommentObjects(ctx, items)
		}
		return
	}

	//所有被物理删除的评论 ID
	removedIDs := make([]int64, 0, len(result.PhysicallyRemoved))
	//需要失效内容索引的 contentID  针对删顶级评论的情况
	contentIDsToInvalidate := make(map[int64]struct{})
	replyRootsToInvalidate := make(map[int64]struct{})
	parentObjectsToDelete := make(map[int64]struct{})

	//收集要删除的缓存信息
	for _, removed := range result.PhysicallyRemoved {
		removedIDs = append(removedIDs, removed.ID)
		if removed.ParentID == 0 {
			contentIDsToInvalidate[removed.ContentID] = struct{}{}
			replyRootsToInvalidate[removed.ID] = struct{}{}
			continue
		}
		if removed.ParentID > 0 {
			parentObjectsToDelete[removed.ParentID] = struct{}{}
		}
		if removed.RootID > 0 {
			parentObjectsToDelete[removed.RootID] = struct{}{}
			replyRootsToInvalidate[removed.RootID] = struct{}{}
		}
	}

	//删除相关缓存
	_ = s.cache.DeleteCommentObjects(ctx, removedIDs)
	for parentID := range parentObjectsToDelete {
		_ = s.cache.DeleteCommentObject(ctx, parentID)
	}
	for contentID := range contentIDsToInvalidate {
		_ = s.cache.InvalidateContentIndex(ctx, contentID)
	}
	for rootID := range replyRootsToInvalidate {
		_ = s.cache.InvalidateReplyIndex(ctx, rootID)
	}
}

func (s *Service) rebuildContentIndex(ctx context.Context, contentID int64) error {
	if s == nil || s.cache == nil || s.repo == nil {
		return nil
	}

	acquired, err := s.cache.TryAcquireContentIndexLock(ctx, contentID)
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}

	comments, err := s.repo.ListActiveTopLevelComments(ctx, contentID)
	if err != nil {
		return err
	}

	commentIDs := make([]int64, 0, len(comments))
	for _, comment := range comments {
		commentIDs = append(commentIDs, comment.ID)
	}

	items, err := s.buildCommentItems(ctx, comments, true)
	if err != nil {
		return err
	}
	if err := s.cache.SetCommentObjects(ctx, items); err != nil {
		return err
	}
	return s.cache.RebuildContentIndex(ctx, contentID, commentIDs)
}

func (s *Service) rebuildReplyIndex(ctx context.Context, rootID int64) error {
	if s == nil || s.cache == nil || s.repo == nil {
		return nil
	}

	acquired, err := s.cache.TryAcquireReplyIndexLock(ctx, rootID)
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}

	comments, err := s.repo.ListActiveReplies(ctx, rootID)
	if err != nil {
		return err
	}

	commentIDs := make([]int64, 0, len(comments))
	for _, comment := range comments {
		commentIDs = append(commentIDs, comment.ID)
	}

	items, err := s.buildCommentItems(ctx, comments, false)
	if err != nil {
		return err
	}
	if err := s.cache.SetCommentObjects(ctx, items); err != nil {
		return err
	}
	return s.cache.RebuildReplyIndex(ctx, rootID, commentIDs)
}

func (s *Service) loadCommentItems(ctx context.Context, commentIDs []int64, includeReplyCount bool) ([]model.CommentItem, error) {
	if len(commentIDs) == 0 {
		return []model.CommentItem{}, nil
	}

	cachedItems, missingIDs, err := s.cache.GetCommentObjects(ctx, commentIDs)
	if err != nil {
		return nil, err
	}

	if len(missingIDs) > 0 {
		comments, err := s.repo.ListActiveCommentsByIDs(ctx, missingIDs)
		if err != nil {
			return nil, err
		}
		rebuiltItems, err := s.buildCommentItems(ctx, comments, includeReplyCount)
		if err != nil {
			return nil, err
		}
		if err := s.cache.SetCommentObjects(ctx, rebuiltItems); err != nil {
			return nil, err
		}
		for _, item := range rebuiltItems {
			cachedItems[item.ID] = item
		}
	}

	items := make([]model.CommentItem, 0, len(commentIDs))
	for _, commentID := range commentIDs {
		if item, ok := cachedItems[commentID]; ok {
			items = append(items, item)
		}
	}
	return items, nil
}

//[]model.CommentItem{
// 	{
// 		ID:            101,
// 		ContentID:     9001,
// 		ContentUserID: 88,
// 		UserID:        1,
// 		ReplyToUserID: 0,
// 		ParentID:      0,
// 		RootID:        0,
// 		Comment:       "第一条顶级评论",
// 		IsDeleted:     false,
// 		ReplyCount:    1,
// 		Author:        &model.UserSummary{UserID: 1, Username: "alice"},
// 		ReplyToUser:   nil,
// 		CreatedAt:     t1,
// 		UpdatedAt:     t1,
// 	},
// 	{
// 		ID:            102,
// 		ContentID:     9001,
// 		ContentUserID: 88,
// 		UserID:        2,
// 		ReplyToUserID: 1,
// 		ParentID:      101,
// 		RootID:        101,
// 		Comment:       "回复一下你",
// 		IsDeleted:     false,
// 		ReplyCount:    0,
// 		Author:        &model.UserSummary{UserID: 2, Username: "bob"},
// 		ReplyToUser:   &model.UserSummary{UserID: 1, Username: "alice"},
// 		CreatedAt:     t2,
// 		UpdatedAt:     t2,
// 	},
// 	{
// 		ID:            103,
// 		ContentID:     9001,
// 		ContentUserID: 88,
// 		UserID:        3,
// 		ReplyToUserID: 0,
// 		ParentID:      0,
// 		RootID:        0,
// 		Comment:       "comment deleted",
// 		IsDeleted:     true,
// 		ReplyCount:    0,
// 		Author:        &model.UserSummary{UserID: 3, Username: "charlie"},
// 		ReplyToUser:   nil,
// 		CreatedAt:     t3,
// 		UpdatedAt:     t4,
// 	},
// }
func (s *Service) buildCommentItems(ctx context.Context, comments []model.Comment, includeReplyCount bool) ([]model.CommentItem, error) {
	if len(comments) == 0 {
		return []model.CommentItem{}, nil
	}

	userIDs := make([]int64, 0, len(comments)*2)
	seenUserIDs := make(map[int64]struct{})
	rootIDs := make([]int64, 0)
	for _, comment := range comments {
		if _, ok := seenUserIDs[comment.UserID]; !ok && comment.UserID > 0 {
			seenUserIDs[comment.UserID] = struct{}{}
			userIDs = append(userIDs, comment.UserID)
		}
		if _, ok := seenUserIDs[comment.ReplyToUserID]; !ok && comment.ReplyToUserID > 0 {
			seenUserIDs[comment.ReplyToUserID] = struct{}{}
			userIDs = append(userIDs, comment.ReplyToUserID)
		}
		if includeReplyCount && comment.ParentID == 0 {
			rootIDs = append(rootIDs, comment.ID)
		}
	}

	userMap := map[int64]model.UserSummary{}
	if s.users != nil && len(userIDs) > 0 {
		var err error
		userMap, err = s.users.BatchGetUserMap(ctx, userIDs)
		if err != nil {
			return nil, err
		}
	}

	replyCountMap := map[int64]int64{}
	if includeReplyCount && len(rootIDs) > 0 {
		var err error
		//在数据库中查rootid下面所有的回复记录
		replyCountMap, err = s.repo.CountRepliesByRootIDs(ctx, rootIDs)
		if err != nil {
			return nil, err
		}
	}

	items := make([]model.CommentItem, 0, len(comments))
	for _, comment := range comments {
		item := model.CommentItem{
			ID:            comment.ID,
			ContentID:     comment.ContentID,
			ContentUserID: comment.ContentUserID,
			UserID:        comment.UserID,
			ReplyToUserID: comment.ReplyToUserID,
			ParentID:      comment.ParentID,
			RootID:        comment.RootID,
			Comment:       comment.Comment,
			IsDeleted:     comment.IsDeleted,
			ReplyCount:    replyCountMap[comment.ID],
			CreatedAt:     comment.CreatedAt,
			UpdatedAt:     comment.UpdatedAt,
		}
		if comment.IsDeleted {
			item.Comment = "comment deleted"
		}
		if author, ok := userMap[comment.UserID]; ok {
			authorCopy := author
			item.Author = &authorCopy
		}
		if replyTo, ok := userMap[comment.ReplyToUserID]; ok {
			replyToCopy := replyTo
			item.ReplyToUser = &replyToCopy
		}
		items = append(items, item)
	}

	return items, nil
}
