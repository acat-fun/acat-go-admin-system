package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// S3 是 MinIO/S3 兼容的对象存储实现（AWS Signature Version 4、path-style 寻址）。
//
// 客户端构造口径：固定 region us-east-1、使用配置的 endpoint 覆盖默认地址、
// 强制 path-style 寻址，并以配置的 access key / secret key 做静态签名。
//
// 已由单元测试覆盖签名规范串构造、路径编码、桶/键解析与错误映射；
// 尚未连接真实 MinIO 做过联调。
type S3 struct {
	endpoint  string
	accessKey string
	secretKey string
	bucket    string
	region    string
	client    *http.Client
	// now 便于测试注入固定时间（SigV4 依赖时间戳）。
	now func() time.Time
}

// S3Options 配置 S3 客户端。
type S3Options struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	// Timeout 单次请求超时，默认 30s。
	Timeout time.Duration
}

// NewS3 构造 S3/MinIO 客户端。
func NewS3(opts S3Options) *S3 {
	region := strings.TrimSpace(opts.Region)
	if region == "" {
		region = "us-east-1"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &S3{
		endpoint:  strings.TrimRight(strings.TrimSpace(opts.Endpoint), "/"),
		accessKey: opts.AccessKey,
		secretKey: opts.SecretKey,
		bucket:    opts.Bucket,
		region:    region,
		client:    &http.Client{Timeout: timeout},
		now:       time.Now,
	}
}

// DefaultBucket 实现 ObjectStorage。
func (s *S3) DefaultBucket() string { return s.bucket }

// Put 实现 ObjectStorage（S3 PutObject）。
func (s *S3) Put(ctx context.Context, bucket, key string, body []byte, contentType string) error {
	response, err := s.do(ctx, http.MethodPut, bucket, key, body, contentType)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("storage: PutObject 失败: HTTP %d", response.StatusCode)
	}
	return nil
}

// Get 实现 ObjectStorage（S3 GetObject）；对象不存在返回 ErrObjectNotFound。
func (s *S3) Get(ctx context.Context, bucket, key string) (*Object, error) {
	response, err := s.do(ctx, http.MethodGet, bucket, key, nil, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return nil, ErrObjectNotFound
	}
	if response.StatusCode/100 != 2 {
		return nil, fmt.Errorf("storage: GetObject 失败: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("storage: 读取对象失败: %w", err)
	}
	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return &Object{Body: body, ContentType: contentType, Size: int64(len(body))}, nil
}

// Delete 实现 ObjectStorage（S3 DeleteObject；S3 对不存在的键同样返回 204，天然幂等）。
func (s *S3) Delete(ctx context.Context, bucket, key string) error {
	response, err := s.do(ctx, http.MethodDelete, bucket, key, nil, "")
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("storage: DeleteObject 失败: HTTP %d", response.StatusCode)
	}
	return nil
}

// do 发送一次签名请求。
func (s *S3) do(ctx context.Context, method, bucket, key string, body []byte, contentType string) (*http.Response, error) {
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("storage: objectKey 不能为空")
	}
	resolvedBucket := s.resolveBucket(bucket)
	requestURL := s.endpoint + "/" + resolvedBucket + "/" + encodePath(key)
	request, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("storage: 构造请求失败: %w", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	s.sign(request, body)
	return s.client.Do(request)
}

func (s *S3) resolveBucket(bucket string) string {
	if strings.TrimSpace(bucket) == "" {
		return s.bucket
	}
	return bucket
}

// sign 计算并写入 AWS Signature Version 4 认证头。
func (s *S3) sign(request *http.Request, body []byte) {
	now := s.now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	payloadHash := sha256Hex(body)
	request.Header.Set("x-amz-content-sha256", payloadHash)
	request.Header.Set("x-amz-date", amzDate)
	request.Header.Set("Host", request.URL.Host)

	canonicalHeaders := "host:" + request.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := strings.Join([]string{
		request.Method,
		canonicalURI(request.URL),
		canonicalQuery(request.URL),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + s.region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := deriveSigningKey(s.secretKey, dateStamp, s.region, "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	request.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey, scope, signedHeaders, signature))
}

// canonicalURI 返回 SigV4 规范 URI：每个路径段单独编码，保留 "/"。
func canonicalURI(target *url.URL) string {
	path := target.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

// canonicalQuery 返回 SigV4 规范查询串（本实现不产生查询参数）。
func canonicalQuery(_ *url.URL) string { return "" }

// encodePath 对对象键做路径编码（保留 "/"，其余按 RFC 3986 非保留字符集编码）。
func encodePath(key string) string {
	segments := strings.Split(key, "/")
	for index, segment := range segments {
		segments[index] = awsURIEncode(segment)
	}
	return strings.Join(segments, "/")
}

// awsURIEncode 按 AWS 规则编码：不编码 A-Za-z0-9-_.~，其余百分号编码（大写十六进制）。
func awsURIEncode(value string) string {
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		char := value[index]
		switch {
		case char >= 'A' && char <= 'Z', char >= 'a' && char <= 'z', char >= '0' && char <= '9',
			char == '-', char == '_', char == '.', char == '~':
			builder.WriteByte(char)
		default:
			builder.WriteString("%")
			builder.WriteString(strings.ToUpper(hex.EncodeToString([]byte{char})))
		}
	}
	return builder.String()
}

func deriveSigningKey(secretKey, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ErrEndpointRequired 表示 S3 端点未配置。
var ErrEndpointRequired = errors.New("storage: endpoint 不能为空")
