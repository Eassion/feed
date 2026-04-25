package uploadsvc

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"feed/internal/upload"
)

const (
	avatarMaxBytes       int64 = 5 << 20
	coverMaxBytes        int64 = 10 << 20
	videoMaxBytes        int64 = 200 << 20
	credentialTTL              = 15 * time.Minute
	LocalUploadFormField       = "file"
)

const (
	UploadSceneAvatar       = "avatar"
	UploadSceneArticleCover = "article_cover"
	UploadSceneVideoCover   = "video_cover"
	UploadSceneVideoSource  = "video_source"
)

var (
	ErrInvalidUploadRequest = errors.New("invalid upload request")
	ErrUnsupportedUpload    = errors.New("unsupported upload file")
	ErrExpiredCredential    = errors.New("upload credential expired")
	ErrInvalidCredential    = errors.New("invalid upload credential")
)

var imageExts = map[string]string{
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"gif":  "image/gif",
	"webp": "image/webp",
}

var imageMimes = setOf("image/jpeg", "image/png", "image/gif", "image/webp")

type Service struct {
	store     *upload.Store
	secret    []byte
	uploadURL string
	now       func() time.Time
}

type AvatarUploadResult struct {
	URL       string `json:"url"`
	ObjectKey string `json:"object_key"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
}

type UploadCredentialRequest struct {
	Scene    string
	FileName string
	Ext      string
	MimeType string
}

type UploadCredential struct {
	ObjectKey string            `json:"object_key"`
	UploadURL string            `json:"upload_url"`
	FormData  map[string]string `json:"form_data"`
	ExpiresAt time.Time         `json:"expired_at"`
}

type ObjectUploadRequest struct {
	ObjectKey string
	Scene     string
	MimeType  string
	ExpiresAt time.Time
	Signature string
}

type ObjectUploadResult struct {
	URL       string `json:"url"`
	ObjectKey string `json:"object_key"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
}

type uploadRule struct {
	baseDir      string
	maxBytes     int64
	allowedExts  map[string]string
	allowedMimes map[string]struct{}
}

func New(store *upload.Store, secret, uploadURL string) *Service {
	return &Service{
		store:     store,
		secret:    []byte(secret),
		uploadURL: uploadURL,
		now:       time.Now,
	}
}

func (s *Service) UploadAvatar(ctx context.Context, fileName string, content io.Reader) (*AvatarUploadResult, error) {
	objectKey, mimeType, size, reader, err := s.prepareObject(UploadSceneAvatar, fileName, content)
	if err != nil {
		return nil, err
	}

	if err := s.store.Save(objectKey, reader); err != nil {
		return nil, err
	}

	return &AvatarUploadResult{
		URL:       s.store.URL(objectKey),
		ObjectKey: objectKey,
		Mime:      mimeType,
		Size:      size,
	}, nil
}

func (s *Service) CreateContentCredential(_ context.Context, req UploadCredentialRequest) (*UploadCredential, error) {
	rule, ext, mimeType, err := s.resolveRule(req.Scene, req.FileName, req.Ext, req.MimeType)
	if err != nil {
		return nil, err
	}

	expiresAt := s.now().Add(credentialTTL).UTC()
	objectKey, err := s.newObjectKey(rule.baseDir, ext)
	if err != nil {
		return nil, err
	}

	signature := s.sign(objectKey, req.Scene, mimeType, expiresAt, rule.maxBytes)
	return &UploadCredential{
		ObjectKey: objectKey,
		UploadURL: s.uploadURL,
		ExpiresAt: expiresAt,
		FormData: map[string]string{
			"key":        objectKey,
			"scene":      req.Scene,
			"mime_type":  mimeType,
			"expires_at": expiresAt.Format(time.RFC3339),
			"max_size":   fmt.Sprintf("%d", rule.maxBytes),
			"signature":  signature,
		},
	}, nil
}

func (s *Service) UploadObject(ctx context.Context, req ObjectUploadRequest, fileName string, content io.Reader) (*ObjectUploadResult, error) {
	if strings.TrimSpace(req.ObjectKey) == "" || strings.TrimSpace(req.Scene) == "" || req.ExpiresAt.IsZero() || strings.TrimSpace(req.Signature) == "" {
		return nil, ErrInvalidUploadRequest
	}
	if s.now().UTC().After(req.ExpiresAt.UTC()) {
		return nil, ErrExpiredCredential
	}

	rule, ext, normalizedMime, err := s.resolveRule(req.Scene, fileName, path.Ext(req.ObjectKey), req.MimeType)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(req.ObjectKey, "."+ext) {
		return nil, ErrInvalidCredential
	}

	expected := s.sign(req.ObjectKey, req.Scene, normalizedMime, req.ExpiresAt.UTC(), rule.maxBytes)
	if !hmac.Equal([]byte(expected), []byte(req.Signature)) {
		return nil, ErrInvalidCredential
	}

	size, actualMime, reader, err := sniffAndLimit(content, rule.maxBytes)
	if err != nil {
		return nil, err
	}
	if actualMime != normalizedMime {
		return nil, ErrUnsupportedUpload
	}

	if err := s.store.Save(req.ObjectKey, reader); err != nil {
		return nil, err
	}

	return &ObjectUploadResult{
		URL:       s.store.URL(req.ObjectKey),
		ObjectKey: req.ObjectKey,
		Mime:      actualMime,
		Size:      size,
	}, nil
}

func (s *Service) TranscodeVideo(ctx context.Context, originURL string) (string, string, error) {
	if s == nil || s.store == nil {
		return "", "", ErrInvalidUploadRequest
	}

	originKey, ok := s.store.ObjectKeyFromURL(originURL)
	if !ok {
		return "", "", ErrInvalidUploadRequest
	}
	if _, _, _, err := s.resolveRule(UploadSceneVideoSource, originKey, "", ""); err != nil {
		return "", "", err
	}

	inputPath, err := s.store.Path(originKey)
	if err != nil {
		return "", "", err
	}

	randomPart, err := randomHex(6)
	if err != nil {
		return "", "", err
	}
	now := s.now().UTC()
	mediaID := fmt.Sprintf("%d_%s", now.UnixMilli(), randomPart)
	playlistKey := fmt.Sprintf("content/video-hls/%s/%s/index.m3u8", now.Format("20060102"), mediaID)
	playlistPath, err := s.store.Path(playlistKey)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(playlistPath), 0o755); err != nil {
		return "", "", err
	}

	segmentPattern := filepath.Join(filepath.Dir(playlistPath), "segment_%05d.ts")
	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-y",
		"-i", inputPath,
		"-c:v", "libx264",
		"-c:a", "aac",
		"-preset", "veryfast",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("ffmpeg transcode failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return s.store.URL(playlistKey), mediaID, nil
}

func (s *Service) Open(objectKey string) (io.ReadSeekCloser, error) {
	return s.store.Open(objectKey)
}

func (s *Service) prepareObject(scene, fileName string, content io.Reader) (string, string, int64, io.Reader, error) {
	rule, ext, _, err := s.resolveRule(scene, fileName, "", "")
	if err != nil {
		return "", "", 0, nil, err
	}

	size, mimeType, reader, err := sniffAndLimit(content, rule.maxBytes)
	if err != nil {
		return "", "", 0, nil, err
	}
	if _, ok := rule.allowedMimes[mimeType]; !ok {
		return "", "", 0, nil, ErrUnsupportedUpload
	}

	objectKey, err := s.newObjectKey(rule.baseDir, ext)
	if err != nil {
		return "", "", 0, nil, err
	}

	return objectKey, mimeType, size, reader, nil
}

func (s *Service) resolveRule(scene, fileName, extOrObjectExt, mimeType string) (uploadRule, string, string, error) {
	rule, ok := uploadRules()[scene]
	if !ok {
		return uploadRule{}, "", "", ErrInvalidUploadRequest
	}

	ext := normalizeExt(fileName, extOrObjectExt)
	if ext == "" {
		return uploadRule{}, "", "", ErrInvalidUploadRequest
	}

	normalizedMime, ok := rule.allowedExts[ext]
	if !ok {
		return uploadRule{}, "", "", ErrUnsupportedUpload
	}

	if mimeType != "" {
		mimeType = normalizeMime(mimeType)
		if mimeType != normalizedMime {
			return uploadRule{}, "", "", ErrUnsupportedUpload
		}
	}

	return rule, ext, normalizedMime, nil
}

func (s *Service) newObjectKey(baseDir, ext string) (string, error) {
	randomPart, err := randomHex(6)
	if err != nil {
		return "", err
	}

	now := s.now().UTC()
	return fmt.Sprintf("%s/%s/%d_%s.%s", baseDir, now.Format("20060102"), now.UnixMilli(), randomPart, ext), nil
}

func (s *Service) sign(objectKey, scene, mimeType string, expiresAt time.Time, maxBytes int64) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = io.WriteString(mac, objectKey)
	_, _ = io.WriteString(mac, "\n")
	_, _ = io.WriteString(mac, scene)
	_, _ = io.WriteString(mac, "\n")
	_, _ = io.WriteString(mac, mimeType)
	_, _ = io.WriteString(mac, "\n")
	_, _ = io.WriteString(mac, expiresAt.UTC().Format(time.RFC3339))
	_, _ = io.WriteString(mac, "\n")
	_, _ = io.WriteString(mac, fmt.Sprintf("%d", maxBytes))
	return hex.EncodeToString(mac.Sum(nil))
}

func sniffAndLimit(content io.Reader, maxBytes int64) (int64, string, io.Reader, error) {
	limited := io.LimitReader(content, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return 0, "", nil, err
	}
	if int64(len(data)) == 0 {
		return 0, "", nil, ErrInvalidUploadRequest
	}
	if int64(len(data)) > maxBytes {
		return 0, "", nil, ErrUnsupportedUpload
	}

	mimeType := normalizeMime(http.DetectContentType(data))
	return int64(len(data)), mimeType, bytes.NewReader(data), nil
}

func uploadRules() map[string]uploadRule {
	return map[string]uploadRule{
		UploadSceneAvatar: {
			baseDir:      "avatar",
			maxBytes:     avatarMaxBytes,
			allowedExts:  imageExts,
			allowedMimes: imageMimes,
		},
		UploadSceneArticleCover: {
			baseDir:      "content/article-cover",
			maxBytes:     coverMaxBytes,
			allowedExts:  imageExts,
			allowedMimes: imageMimes,
		},
		UploadSceneVideoCover: {
			baseDir:      "content/video-cover",
			maxBytes:     coverMaxBytes,
			allowedExts:  imageExts,
			allowedMimes: imageMimes,
		},
		UploadSceneVideoSource: {
			baseDir:  "content/video",
			maxBytes: videoMaxBytes,
			allowedExts: map[string]string{
				"mp4":  "video/mp4",
				"mov":  "video/quicktime",
				"m4v":  "video/x-m4v",
				"webm": "video/webm",
			},
			allowedMimes: setOf("video/mp4", "video/quicktime", "video/x-m4v", "video/webm"),
		},
	}
}

func normalizeExt(fileName, fallbackExt string) string {
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(strings.TrimSpace(fileName))), ".")
	if ext != "" {
		return ext
	}
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fallbackExt)), ".")
}

func normalizeMime(mimeType string) string {
	return strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func setOf(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
