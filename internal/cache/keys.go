package cache

import (
	"fmt"
	"time"
)

const (
	FeedHotGlobalKey         = "feed:hot:global"
	FeedHotLatestSnapshotKey = "feed:hot:global:latest"
	FeedHotShardCount        = int64(64)
	FollowInboxKeepN         = int64(5000)
	FollowInboxRebuildLimit  = int64(500)
	UserPublishKeepN         = int64(5000)
	UserPublishRebuildLimit  = int64(500)
	UserFavoriteKeepN        = int64(5000)
	UserFavoriteRebuildLimit = int64(500)
	CountCacheTTL            = 24 * time.Hour
)

const (
	FollowInboxRebuildLockTTL  = 30 * time.Second
	UserPublishRebuildLockTTL  = 30 * time.Second
	UserFavoriteRebuildLockTTL = 30 * time.Second
	CommentIndexRebuildLockTTL = 30 * time.Second
	UserProfileRebuildLockTTL  = 30 * time.Second
)

func FeedHotSnapshotKey(snapshotID string) string {
	return fmt.Sprintf("feed:hot:global:snap:%s", snapshotID)
}

func FeedHotGlobalIncKey(shard int64) string {
	return fmt.Sprintf("feed:hot:global:inc:%d", shard)
}

func FeedHotGlobalFastLockKey(bucket string) string {
	return fmt.Sprintf("feed:hot:global:lock:fast:%s", bucket)
}

func FeedHotGlobalColdLockKey(date string) string {
	return fmt.Sprintf("feed:hot:global:lock:cold:%s", date)
}

func UserSessionTokenKey(token string) string {
	return fmt.Sprintf("user:session:%s", token)
}

func UserSessionUserKey(userID int64) string {
	return fmt.Sprintf("user:session:user:%d", userID)
}

func LikeUserKey(userID int64) string {
	return fmt.Sprintf("like:user:%d", userID)
}

func LikeActionKey(scene string, contentID int64) string {
	return fmt.Sprintf("like:action:%s:%d", scene, contentID)
}

func LikeCountKey(scene string, contentID int64) string {
	return fmt.Sprintf("like:count:%s:%d", scene, contentID)
}

func FavoriteUserKey(userID int64) string {
	return fmt.Sprintf("favorite:user:%d", userID)
}

func FollowUserKey(userID int64) string {
	return fmt.Sprintf("follow:user:%d", userID)
}

func FollowInboxKey(userID int64) string {
	return fmt.Sprintf("feed:follow:inbox:%d", userID)
}

func FollowInboxInitKey(userID int64) string {
	return fmt.Sprintf("feed:follow:inbox:init:%d", userID)
}

func FollowInboxRebuildLockKey(userID int64) string {
	return fmt.Sprintf("feed:follow:inbox:lock:%d", userID)
}

func UserPublishKey(userID int64) string {
	return fmt.Sprintf("feed:user:publish:%d", userID)
}

func UserPublishInitKey(userID int64) string {
	return fmt.Sprintf("feed:user:publish:init:%d", userID)
}

func UserPublishRebuildLockKey(userID int64) string {
	return fmt.Sprintf("feed:user:publish:lock:%d", userID)
}

func UserFavoriteKey(userID int64) string {
	return fmt.Sprintf("feed:user:favorite:%d", userID)
}

func UserFavoriteInitKey(userID int64) string {
	return fmt.Sprintf("feed:user:favorite:init:%d", userID)
}

func UserFavoriteRebuildLockKey(userID int64) string {
	return fmt.Sprintf("feed:user:favorite:lock:%d", userID)
}

func CountValueKey(bizType, targetType int32, targetID int64) string {
	return fmt.Sprintf("count:value:%d:%d:%d", bizType, targetType, targetID)
}

func CountRebuildLockKey(bizType, targetType int32, targetID int64) string {
	return fmt.Sprintf("lock:rebuild:count:%d:%d:%d", bizType, targetType, targetID)
}

func UserProfileCountKey(userID int64) string {
	return fmt.Sprintf("count:user:profile:%d", userID)
}

func UserProfileCountLockKey(userID int64) string {
	return fmt.Sprintf("lock:rebuild:count:user:profile:%d", userID)
}

func CanalCountDedupKey(eventID string) string {
	return fmt.Sprintf("count:canal:dedup:%s", eventID)
}

func CommentObjectKey(commentID int64) string {
	return fmt.Sprintf("comment:obj:%d", commentID)
}

func CommentContentIndexKey(contentID int64) string {
	return fmt.Sprintf("comment:idx:content:%d", contentID)
}

func CommentContentIndexInitKey(contentID int64) string {
	return fmt.Sprintf("comment:idx:content:init:%d", contentID)
}

func CommentContentIndexLockKey(contentID int64) string {
	return fmt.Sprintf("comment:idx:content:lock:%d", contentID)
}

func CommentRootRepliesKey(rootID int64) string {
	return fmt.Sprintf("comment:idx:root:%d", rootID)
}

func CommentRootRepliesInitKey(rootID int64) string {
	return fmt.Sprintf("comment:idx:root:init:%d", rootID)
}

func CommentRootRepliesLockKey(rootID int64) string {
	return fmt.Sprintf("comment:idx:root:lock:%d", rootID)
}
