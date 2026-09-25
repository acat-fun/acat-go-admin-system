package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMemoryPutGetDelete(t *testing.T) {
	store := NewMemory("acat-local")
	ctx := context.Background()

	if err := store.Put(ctx, "", "acat-fun/read/file/a.txt", []byte("hello"), "text/plain"); err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	object, err := store.Get(ctx, "", "acat-fun/read/file/a.txt")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if string(object.Body) != "hello" || object.Size != 5 || object.ContentType != "text/plain" {
		t.Fatalf("对象内容异常: %+v", object)
	}
	if keys := store.Keys(); len(keys) != 1 || keys[0] != "acat-local/acat-fun/read/file/a.txt" {
		t.Fatalf("键列表异常: %v", keys)
	}
	if err := store.Delete(ctx, "acat-local", "acat-fun/read/file/a.txt"); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, err := store.Get(ctx, "acat-local", "acat-fun/read/file/a.txt"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("删除后应返回 ErrObjectNotFound，实际 %v", err)
	}
}

func TestMemoryIsolatesBucketsAndCopiesBody(t *testing.T) {
	store := NewMemory("bucket-a")
	ctx := context.Background()
	body := []byte("payload")
	if err := store.Put(ctx, "bucket-b", "k", body, ""); err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	// 调用方复用缓冲区不应影响已存对象。
	body[0] = 'X'
	object, err := store.Get(ctx, "bucket-b", "k")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if string(object.Body) != "payload" {
		t.Fatalf("对象内容被外部缓冲区改写: %q", object.Body)
	}
	if object.ContentType != "application/octet-stream" {
		t.Fatalf("空 Content-Type 应回退 octet-stream，实际 %q", object.ContentType)
	}
	if _, err := store.Get(ctx, "bucket-a", "k"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("跨桶不应命中，实际 %v", err)
	}
}

func TestMemoryRejectsEmptyKey(t *testing.T) {
	store := NewMemory("b")
	if err := store.Put(context.Background(), "b", " ", []byte("x"), ""); err == nil {
		t.Fatal("空 objectKey 应报错")
	}
}

func TestAWSURIEncode(t *testing.T) {
	cases := map[string]string{
		"a.txt":            "a.txt",
		"acat-fun/read":    "acat-fun%2Fread",
		"中文":               "%E4%B8%AD%E6%96%87",
		"a b+c":            "a%20b%2Bc",
		"A-Z_a-z.0~9":      "A-Z_a-z.0~9",
		"!*'();:@&=+$,/?#": "%21%2A%27%28%29%3B%3A%40%26%3D%2B%24%2C%2F%3F%23",
	}
	for input, want := range cases {
		if got := awsURIEncode(input); got != want {
			t.Errorf("awsURIEncode(%q) = %q, 期望 %q", input, got, want)
		}
	}
}

func TestEncodePathKeepsSeparators(t *testing.T) {
	got := encodePath("acat-fun/read/file/中文 名.txt")
	want := "acat-fun/read/file/%E4%B8%AD%E6%96%87%20%E5%90%8D.txt"
	if got != want {
		t.Fatalf("encodePath = %q, 期望 %q", got, want)
	}
}

func TestDeriveSigningKeyIsDeterministic(t *testing.T) {
	first := deriveSigningKey("secret", "20260914", "us-east-1", "s3")
	second := deriveSigningKey("secret", "20260914", "us-east-1", "s3")
	if len(first) != 32 || string(first) != string(second) {
		t.Fatalf("签名密钥不稳定: %x vs %x", first, second)
	}
	if string(deriveSigningKey("secret", "20260915", "us-east-1", "s3")) == string(first) {
		t.Fatal("日期变化应导致签名密钥变化")
	}
	if string(deriveSigningKey("secret", "20260914", "us-west-2", "s3")) == string(first) {
		t.Fatal("区域变化应导致签名密钥变化")
	}
}

// TestS3SignsRequestsAndMapsResponses 用 httptest 充当 MinIO，验证：
//   - path-style URL（/{bucket}/{key}）；
//   - Authorization 头包含 SigV4 必需字段；
//   - 404 → ErrObjectNotFound；DELETE 幂等（204）。
//
// 注意：这是本地替身服务器，不是真实 MinIO 联调（本机无 MinIO，标注「待 UAT 验证」）。
func TestS3SignsRequestsAndMapsResponses(t *testing.T) {
	var (
		gotAuth   string
		gotPath   string
		gotMethod string
		gotBody   string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		if r.Method == http.MethodPut {
			buffer := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(buffer)
			gotBody = string(buffer)
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/missing.txt") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewS3(S3Options{
		Endpoint:  server.URL,
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Bucket:    "acat-local",
	})
	client.now = func() time.Time { return time.Date(2026, 9, 14, 1, 2, 3, 0, time.UTC) }
	ctx := context.Background()

	if err := client.Put(ctx, "", "acat-fun/read/file/a.txt", []byte("data"), "text/plain"); err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/acat-local/acat-fun/read/file/a.txt" || gotBody != "data" {
		t.Fatalf("Put 请求异常: method=%s path=%s body=%q", gotMethod, gotPath, gotBody)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=minioadmin/20260914/us-east-1/s3/aws4_request") {
		t.Fatalf("Authorization 头异常: %q", gotAuth)
	}
	if !strings.Contains(gotAuth, "SignedHeaders=host;x-amz-content-sha256;x-amz-date") ||
		!strings.Contains(gotAuth, "Signature=") {
		t.Fatalf("Authorization 头缺少签名要素: %q", gotAuth)
	}

	if _, err := client.Get(ctx, "", "acat-fun/read/file/missing.txt"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("404 应映射为 ErrObjectNotFound，实际 %v", err)
	}
	if err := client.Delete(ctx, "", "acat-fun/read/file/a.txt"); err != nil {
		t.Fatalf("Delete 应幂等成功: %v", err)
	}
}

func TestS3DefaultBucketAndRegion(t *testing.T) {
	client := NewS3(S3Options{Endpoint: "http://127.0.0.1:9003", Bucket: "acat"})
	if client.DefaultBucket() != "acat" || client.region != "us-east-1" {
		t.Fatalf("默认桶/区域异常: %s/%s", client.DefaultBucket(), client.region)
	}
}
