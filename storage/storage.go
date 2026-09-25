// Package storage 定义对象存储（MinIO/S3 兼容）的可注入接口与实现。
//
// 对象存储依赖被收敛为 ObjectStorage 接口，便于：
//   - 单元测试注入内存实现（不需要 MinIO）；
//   - 生产按配置切换到真实 S3/MinIO 实现。
//
// 两个实现：
//   - Memory：进程内内存实现，仅本地联调/单测使用，重启即丢数据；
//   - S3：MinIO/S3 兼容实现（自研 SigV4 客户端，无新增第三方依赖）。
//
// S3 实现由单元测试覆盖（签名规范串构造、路径编码、桶/键解析、错误映射），
// 尚未连接真实 MinIO 做过联调。
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// ErrObjectNotFound 表示对象不存在。
var ErrObjectNotFound = errors.New("storage: 对象不存在")

// Object 是读出的对象内容与元信息。
type Object struct {
	// Body 是对象内容。
	Body []byte
	// ContentType 是对象存储记录的 Content-Type；可能与数据库中的 type 不一致。
	ContentType string
	// Size 是对象字节数。
	Size int64
}

// ObjectStorage 是对象存储的最小能力集：
// 写入对象、读取对象、删除对象与读取默认桶名。
type ObjectStorage interface {
	// Put 写入对象；bucket 为空时使用默认桶。
	Put(ctx context.Context, bucket, key string, body []byte, contentType string) error
	// Get 读取对象；对象不存在返回 ErrObjectNotFound。
	Get(ctx context.Context, bucket, key string) (*Object, error)
	// Delete 删除对象；对象不存在返回 ErrObjectNotFound。
	Delete(ctx context.Context, bucket, key string) error
	// DefaultBucket 返回默认桶名。
	DefaultBucket() string
}

// Memory 是线程安全的内存对象存储实现。
//
// 仅用于本地联调与单元测试：进程退出即丢数据，多副本部署不共享。
type Memory struct {
	mu     sync.RWMutex
	bucket string
	// objects 以 "bucket/key" 为键。
	objects map[string]memoryObject
}

type memoryObject struct {
	body        []byte
	contentType string
}

// NewMemory 构造内存对象存储。
func NewMemory(defaultBucket string) *Memory {
	if strings.TrimSpace(defaultBucket) == "" {
		defaultBucket = "acat"
	}
	return &Memory{bucket: defaultBucket, objects: make(map[string]memoryObject)}
}

// DefaultBucket 实现 ObjectStorage。
func (m *Memory) DefaultBucket() string { return m.bucket }

// Put 实现 ObjectStorage。
func (m *Memory) Put(_ context.Context, bucket, key string, body []byte, contentType string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("storage: objectKey 不能为空")
	}
	resolved := m.resolveBucket(bucket)
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	// 复制一份，避免调用方复用缓冲区导致内容被改写。
	copied := make([]byte, len(body))
	copy(copied, body)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[resolved+"/"+key] = memoryObject{body: copied, contentType: contentType}
	return nil
}

// Get 实现 ObjectStorage。
func (m *Memory) Get(_ context.Context, bucket, key string) (*Object, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	object, ok := m.objects[m.resolveBucket(bucket)+"/"+key]
	if !ok {
		return nil, ErrObjectNotFound
	}
	return &Object{Body: object.body, ContentType: object.contentType, Size: int64(len(object.body))}, nil
}

// Delete 实现 ObjectStorage。
func (m *Memory) Delete(_ context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	full := m.resolveBucket(bucket) + "/" + key
	if _, ok := m.objects[full]; !ok {
		return ErrObjectNotFound
	}
	delete(m.objects, full)
	return nil
}

// Keys 返回当前所有对象的 "bucket/key"，按字典序排列（测试断言用）。
func (m *Memory) Keys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.objects))
	for key := range m.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (m *Memory) resolveBucket(bucket string) string {
	if strings.TrimSpace(bucket) == "" {
		return m.bucket
	}
	return bucket
}

// NewReader 把字节切片包装为 io.Reader（便于调用方流式写出）。
func NewReader(body []byte) io.Reader { return bytes.NewReader(body) }
