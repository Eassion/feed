package cache

import "fmt"

const (
	FeedHotGlobalKey = "feed:hot:global"
)

func FeedHotSnapshotKey(snapshotID string) string {
	return fmt.Sprintf("feed:hot:global:snap:%s", snapshotID)
}

func UserSessionTokenKey(token string) string {
	return fmt.Sprintf("user:session:%s", token)
}

func UserSessionUserKey(userID int64) string {
	return fmt.Sprintf("user:session:user:%d", userID)
}
