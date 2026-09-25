package domain

import "errors"

// 本文件定义文件域的实体与视图（表 t_acat_file），
// 以及文件业务类型白名单与对象存储路径规则。

// 文件业务类型白名单。
const (
	FileTypeOther        = "other"
	FileTypeAvatar       = "avatar"
	FileTypeBookCover    = "book_cover"
	FileTypeComicCover   = "comic_cover"
	FileTypeComicPage    = "comic_page"
	FileTypeAuthorSample = "author_sample"
)

// allowedFileTypes。
var allowedFileTypes = map[string]struct{}{
	FileTypeOther:        {},
	FileTypeAvatar:       {},
	FileTypeBookCover:    {},
	FileTypeComicCover:   {},
	FileTypeComicPage:    {},
	FileTypeAuthorSample: {},
}

// AllowFileType 判断业务文件类型是否在白名单内。
func AllowFileType(fileType string) bool {
	_, ok := allowedFileTypes[fileType]
	return ok
}

// AdminFileEntity 是平台文件实体。
type AdminFileEntity struct {
	ID        string  `json:"id"`
	IsDeleted *int    `json:"isDeleted"`
	CreateBy  *string `json:"createBy"`
	UpdateBy  *string `json:"updateBy"`
	CreatedAt *string `json:"createdAt"`
	UpdatedAt *string `json:"updatedAt"`
	Version   *int    `json:"version"`

	Name     string `json:"name"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	FileType string `json:"fileType"`
	Size     int64  `json:"size"`
}

// AdminFileVO。
type AdminFileVO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Type      string `json:"type"`
	FileType  string `json:"fileType"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// File 是 t_acat_file 的内部读模型（时间字段已格式化为 JSON 文本）。
type File struct {
	ID        string
	Name      string
	Path      string
	Type      string
	FileType  string
	Size      int64
	CreatedAt string
	UpdatedAt string
}

// ToVO。
func (f File) ToVO() AdminFileVO {
	return AdminFileVO{
		ID:        f.ID,
		Name:      f.Name,
		Path:      f.Path,
		Type:      f.Type,
		FileType:  f.FileType,
		Size:      f.Size,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
}

// StorageLocation。
type StorageLocation struct {
	Bucket    string
	ObjectKey string
}

// errEmptyStoragePath。
// IllegalArgumentException("文件存储路径不能为空")。
var errEmptyStoragePath = errors.New("文件存储路径不能为空")

// ResolveStorageLocation。
// db 中的 path 形如 `bucket/objectKey`，首个 `/` 之前是 bucket；
// 没有 `/`（或 `/` 在首尾）时整串作为 key，使用默认桶。
func ResolveStorageLocation(persistedPath, defaultBucket string) (StorageLocation, error) {
	if persistedPath == "" {
		return StorageLocation{}, errEmptyStoragePath
	}
	index := indexByte(persistedPath, '/')
	if index <= 0 || index == len(persistedPath)-1 {
		return StorageLocation{Bucket: defaultBucket, ObjectKey: persistedPath}, nil
	}
	return StorageLocation{
		Bucket:    persistedPath[:index],
		ObjectKey: persistedPath[index+1:],
	}, nil
}

func indexByte(value string, target byte) int {
	for index := 0; index < len(value); index++ {
		if value[index] == target {
			return index
		}
	}
	return -1
}
