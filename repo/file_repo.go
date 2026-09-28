package repo

import (
	"context"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// MySQLFileRepo 是 FileRepo 的 MySQL 实现。
type MySQLFileRepo struct {
	db DBTX
}

// NewFileRepo 构造 MySQL 实现。
func NewFileRepo(db DBTX) *MySQLFileRepo { return &MySQLFileRepo{db: db} }

// fileColumns 是文件查询列清单，不含 is_deleted/create_by/update_by。
const fileColumns = "id, name, path, type, file_type, size, created_at, updated_at"

// ListFiles 实现 FileRepo。
func (r *MySQLFileRepo) ListFiles(ctx context.Context, fileType, pathKeyword string, pageIndex, pageSize int) ([]domain.FileRecord, int64, error) {
	query := "SELECT " + fileColumns + " FROM t_acat_file WHERE is_deleted = 0"
	countQuery := "SELECT COUNT(*) FROM t_acat_file WHERE is_deleted = 0"
	args := []any{}
	if trimSpace(fileType) != "" {
		query += " AND file_type = ?"
		countQuery += " AND file_type = ?"
		args = append(args, fileType)
	}
	if trimmed := trimSpace(pathKeyword); trimmed != "" {
		query += " AND (path LIKE ? OR name LIKE ?)"
		countQuery += " AND (path LIKE ? OR name LIKE ?)"
		pattern := "%" + trimmed + "%"
		args = append(args, pattern, pattern)
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, wrapError("统计文件失败", err)
	}

	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	rows, err := r.db.QueryContext(ctx, query, append(args, pageSize, offset(pageIndex, pageSize))...)
	if err != nil {
		return nil, 0, wrapError("查询文件失败", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.FileRecord, 0, 16)
	for rows.Next() {
		var record domain.FileRecord
		if err := rows.Scan(&record.ID, &record.Name, &record.Path, &record.Type,
			&record.FileType, &record.Size, &record.CreatedAt, &record.UpdatedAt); err != nil {
			return nil, 0, wrapError("扫描文件失败", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, wrapError("遍历文件失败", err)
	}
	return out, total, nil
}

// FindFileByID 实现 FileRepo（WHERE id=? AND is_deleted=0）。
func (r *MySQLFileRepo) FindFileByID(ctx context.Context, id string) (*domain.FileRecord, error) {
	query := "SELECT " + fileColumns + " FROM t_acat_file WHERE id = ? AND is_deleted = 0"
	var record domain.FileRecord
	err := r.db.QueryRowContext(ctx, query, id).Scan(&record.ID, &record.Name, &record.Path,
		&record.Type, &record.FileType, &record.Size, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		return nil, wrapError("查询文件失败", err)
	}
	return &record, nil
}

// InsertFile 实现 FileRepo（is_deleted=0）。
func (r *MySQLFileRepo) InsertFile(ctx context.Context, record domain.FileRecord) error {
	const query = `INSERT INTO t_acat_file
		(id, name, path, type, file_type, size, is_deleted, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, 0)`
	_, err := r.db.ExecContext(ctx, query, record.ID, record.Name, record.Path, record.Type,
		record.FileType, record.Size, record.CreatedAt, record.UpdatedAt)
	return wrapError("新增文件记录失败", err)
}

// SoftDeleteFile 实现 FileRepo（逻辑删除）。
func (r *MySQLFileRepo) SoftDeleteFile(ctx context.Context, id string) (int64, error) {
	const query = `UPDATE t_acat_file SET is_deleted = 1, updated_at = ? WHERE id = ? AND is_deleted = 0`
	result, err := r.db.ExecContext(ctx, query, domain.Now(), id)
	if err != nil {
		return 0, wrapError("删除文件记录失败", err)
	}
	return result.RowsAffected()
}
