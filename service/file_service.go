package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"47.108.230.93/acat-fun/acat-go-admin-system/domain"
	"47.108.230.93/acat-fun/acat-go-admin-system/repo"
	"47.108.230.93/acat-fun/acat-go-admin-system/storage"
	"github.com/acat-fun/acat-go-common/apperr"
)

// safeExtensionPattern。
var safeExtensionPattern = regexp.MustCompile(`^[a-z0-9]{1,10}$`)

// MessageInternal 500 兜底文案（不回显 cause）。
const MessageInternal = "服务器内部错误"

// ValidateFileType。
func ValidateFileType(fileType string) (string, error) {
	value := fileType
	if strings.TrimSpace(value) == "" {
		value = domain.FileTypeOther
	}
	if !domain.AllowFileType(value) {
		return "", business(MessageFileTypeUnsupported + value)
	}
	return value, nil
}

// ListFiles。
func (s *Service) ListFiles(ctx context.Context, pageIndex, pageSize int, fileType string) (any, error) {
	records, total, err := s.files.ListFiles(ctx, fileType, pageIndex, pageSize)
	if err != nil {
		return nil, err
	}
	items := make([]domain.AdminFileVO, 0, len(records))
	for _, record := range records {
		items = append(items, fileRecordToVO(record))
	}
	return newPageData(items, total, pageIndex, pageSize), nil
}

// GetFile。
func (s *Service) GetFile(ctx context.Context, id string) (*domain.File, error) {
	record, err := s.files.FindFileByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, business(MessageFileNotFound)
		}
		return nil, err
	}
	file := fileRecordToDomain(*record)
	return &file, nil
}

// UploadFile。
func (s *Service) UploadFile(ctx context.Context, rc RequestContext, fileType, originalName, contentType string, body []byte) (*domain.AdminFileVO, error) {
	validated, err := ValidateFileType(fileType)
	if err != nil {
		return nil, err
	}
	objectKey := generateObjectKey(validated, originalName)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if err := s.objects.Put(ctx, s.objects.DefaultBucket(), objectKey, body, contentType); err != nil {
		return nil, apperr.Internal(err, MessageInternal)
	}
	name := originalName
	if strings.TrimSpace(name) == "" {
		name = "unknown"
	}
	now := s.now()
	record := domain.FileRecord{
		ID:        s.nextID(),
		Name:      name,
		Path:      s.objects.DefaultBucket() + "/" + objectKey,
		Type:      contentType,
		FileType:  validated,
		Size:      int64(len(body)),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.files.InsertFile(ctx, record); err != nil {
		if cleanupErr := s.objects.Delete(context.WithoutCancel(ctx), s.objects.DefaultBucket(), objectKey); cleanupErr != nil {
			return nil, apperr.Internal(errors.Join(err, cleanupErr), MessageInternal)
		}
		return nil, apperr.Internal(err, MessageInternal)
	}
	vo := fileRecordToVO(record)
	return &vo, nil
}

// DeleteFile。
func (s *Service) DeleteFile(ctx context.Context, id string, removeFromStorage bool) error {
	record, err := s.files.FindFileByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return business(MessageFileNotFound)
		}
		return err
	}
	if removeFromStorage {
		location, locErr := domain.ResolveStorageLocation(record.Path, s.objects.DefaultBucket())
		if locErr != nil {
			return apperr.Internal(locErr, MessageInternal)
		}
		if err := s.objects.Delete(ctx, location.Bucket, location.ObjectKey); err != nil {
			return apperr.Internal(err, MessageInternal)
		}
	}
	deleted, err := s.files.SoftDeleteFile(ctx, id)
	if err != nil {
		return err
	}
	// 删除/撤销类：记录已不存在即达到目标终态，按幂等成功处理。
	s.warnIfNotUpdated(writeFact("deleteFile", TableFile, id, nil, deleted))
	return nil
}

// OpenObject 读取文件对象字节流（供 GET /{id} 与 GET /s/{id} 裸流输出）。
//
// Go 侧同样返回 500，由 httpapi 写出 `{"code":500,"msg":"获取文件流失败"}`。
func (s *Service) OpenObject(ctx context.Context, file domain.File) ([]byte, error) {
	location, err := domain.ResolveStorageLocation(file.Path, s.objects.DefaultBucket())
	if err != nil {
		return nil, apperr.Internal(err, MessageInternal)
	}
	object, err := s.objects.Get(ctx, location.Bucket, location.ObjectKey)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return nil, apperr.Internal(err, MessageInternal)
		}
		return nil, apperr.Internal(err, MessageInternal)
	}
	return object.Body, nil
}

// generateObjectKey。
// 固定前缀 + UUID v4 + 可选的安全扩展名（[a-z0-9]{1,10}）。
func generateObjectKey(fileType, originalName string) string {
	prefix := "acat-fun/read/file"
	switch fileType {
	case domain.FileTypeBookCover:
		prefix = "acat-fun/read/book/cover"
	case domain.FileTypeComicCover:
		prefix = "acat-fun/read/comic/cover"
	case domain.FileTypeComicPage:
		prefix = "acat-fun/read/comic/chapter"
	case domain.FileTypeAvatar:
		prefix = "acat-fun/read/user/avatar"
	case domain.FileTypeAuthorSample:
		prefix = "acat-fun/read/author/sample"
	}
	extension := safeExtension(originalName)
	if extension == "" {
		return prefix + "/" + newUUIDv4()
	}
	return prefix + "/" + newUUIDv4() + "." + extension
}

// safeExtension。
func safeExtension(originalName string) string {
	index := strings.LastIndex(originalName, ".")
	if index < 0 || index == len(originalName)-1 {
		return ""
	}
	extension := strings.ToLower(originalName[index+1:])
	if !safeExtensionPattern.MatchString(extension) {
		return ""
	}
	return extension
}

// newUUIDv4 生成 36 位小写带连字符的 UUID v4。
//
// 注意：对象键里的随机段是 v4，与数据库主键使用的 v7 不同。
func newUUIDv4() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand 在 Go 1.24+ 不会返回错误；保底回落到时间有序 UUID v7，避免空键。
		return domain.NewID()
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	buf := make([]byte, 36)
	hex.Encode(buf[0:8], raw[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], raw[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], raw[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], raw[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], raw[10:16])
	return string(buf)
}

func fileRecordToDomain(record domain.FileRecord) domain.File {
	return domain.File{
		ID:        record.ID,
		Name:      record.Name,
		Path:      record.Path,
		Type:      record.Type,
		FileType:  record.FileType,
		Size:      record.Size,
		CreatedAt: domain.FormatDateTime(record.CreatedAt),
		UpdatedAt: domain.FormatDateTime(record.UpdatedAt),
	}
}

func fileRecordToVO(record domain.FileRecord) domain.AdminFileVO {
	return fileRecordToDomain(record).ToVO()
}
