package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/acat-fun/acat-go-admin-system/domain"
)

// auditBodyCaptureLimit 是审计摘要读取请求体的上限：
const auditBodyCaptureLimit = 64 << 10

// requestParams 近似复刻 AuditLogAspect.buildParams(joinPoint.getArgs())：
//
//	把 Controller 方法入参序列化为 JSON；
//	      单参数直接序列化该对象，多参数序列化为数组；超过 2000 字符截断并追加 "..."。
//	Go：HTTP 中间件拿不到方法入参，改用「请求体 → 路径变量 → 查询参数」重建同量级摘要：
//	      有请求体时以请求体（JSON 原文 / 表单对象）为主；
//	      无请求体时按「路径变量（模式顺序）+ 查询参数（键排序）」组装，单值序列化为标量、
//	      多值序列化为数组
//	      multipart 上传不落文件内容。
//
// 差异已在 README「审计写入路径 · 与 AOP 的差异」登记。
func requestParams(req *http.Request) string {
	mediaType := mediaTypeOf(req.Header.Get("Content-Type"))
	if strings.HasPrefix(mediaType, "multipart/form-data") {
		return "[multipart/form-data]"
	}
	body, truncated := captureRequestBody(req, mediaType)
	if text := paramsFromBody(mediaType, body, truncated); text != "" {
		return truncateParams(text)
	}
	return truncateParams(paramsFromRequestMeta(req))
}

// mediaTypeOf 解析 Content-Type 的媒体类型部分（小写，无参数）。
func mediaTypeOf(contentType string) string {
	if strings.TrimSpace(contentType) == "" {
		return ""
	}
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
		return strings.ToLower(parsed)
	}
	return strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
}

// captureRequestBody 读取请求体前缀并把读到的内容**放回**请求体，保证 handler 仍能完整读取。
//
// 返回读到的字节与「是否因超过上限而截断」；multipart 已在调用方短路，不在此读取。
func captureRequestBody(req *http.Request, mediaType string) ([]byte, bool) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, false
	}
	if mediaType != "" && !isTextLikeMediaType(mediaType) {
		return nil, false
	}
	original := req.Body
	captured, err := io.ReadAll(io.LimitReader(original, auditBodyCaptureLimit+1))
	// 无论成功与否都把已读部分拼回请求体，避免影响业务 handler。
	req.Body = &replayedBody{Reader: io.MultiReader(bytes.NewReader(captured), original), closer: original}
	if err != nil {
		return nil, false
	}
	if len(captured) > auditBodyCaptureLimit {
		return captured[:auditBodyCaptureLimit], true
	}
	return captured, false
}

// replayedBody 把「已读前缀 + 剩余流」重新包装成请求体。
type replayedBody struct {
	io.Reader
	closer io.Closer
}

// Close 关闭底层请求体。
func (b *replayedBody) Close() error { return b.closer.Close() }

// isTextLikeMediaType 判断媒体类型是否可安全按文本摘要记录。
func isTextLikeMediaType(mediaType string) bool {
	switch {
	case mediaType == "":
		// 未声明 Content-Type 时按文本处理（httptest 与 curl -d 常见）。
		return true
	case mediaType == "application/json", mediaType == "text/json", mediaType == "text/plain":
		return true
	case strings.HasSuffix(mediaType, "+json"):
		return true
	case mediaType == "application/x-www-form-urlencoded":
		return true
	default:
		return false
	}
}

// paramsFromBody 按请求体构造参数摘要；返回空串表示「无请求体，改用路径/查询参数」。
func paramsFromBody(mediaType string, body []byte, truncated bool) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}
	if truncated {
		// 超长请求体只保留了前缀：若前缀仍是合法 JSON 则原样使用，否则退化为不完整原文。
		if json.Valid(body) {
			return text
		}
	}
	if mediaType == "application/x-www-form-urlencoded" {
		values, err := url.ParseQuery(text)
		if err != nil {
			return text
		}
		return marshalJSON(formValuesObject(values))
	}
	return text
}

// paramsFromRequestMeta 无请求体时用路径变量与查询参数实现。
func paramsFromRequestMeta(req *http.Request) string {
	values := orderedArgumentValues(req)
	switch len(values) {
	case 0:
		return ""
	case 1:
		return marshalJSON(values[0])
	default:
		return marshalJSON(values)
	}
}

// orderedArgumentValues 组装「路径变量（模式顺序）+ 查询参数（键排序）」参数列表。
func orderedArgumentValues(req *http.Request) []any {
	values := make([]any, 0, 4)
	if req.Pattern != "" {
		// Go 1.23+ 在路由匹配后写入 Pattern，形如 "/api/admin/system/dicts/{id}"。
		for _, name := range patternVariables(req.Pattern) {
			if value := req.PathValue(name); value != "" {
				values = append(values, value)
			}
		}
	}
	query := req.URL.Query()
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		raw := query[key]
		if len(raw) == 1 {
			values = append(values, heuristicValue(raw[0]))
			continue
		}
		list := make([]any, 0, len(raw))
		for _, item := range raw {
			list = append(list, heuristicValue(item))
		}
		values = append(values, list)
	}
	return values
}

// patternVariables 从 ServeMux 模式中提取 `{name}` 变量名（按出现顺序）。
func patternVariables(pattern string) []string {
	names := make([]string, 0, 2)
	for {
		start := strings.Index(pattern, "{")
		if start < 0 {
			return names
		}
		rest := pattern[start+1:]
		end := strings.Index(rest, "}")
		if end < 0 {
			return names
		}
		name := rest[:end]
		// 去掉 "..." 通配与 "/" 结尾的形式，只保留普通变量名。
		name = strings.TrimSuffix(name, "...")
		if name != "" && !strings.ContainsAny(name, "/{}") {
			names = append(names, name)
		}
		pattern = rest[end+1:]
	}
}

// heuristicValue 按常见绑定类型还原查询参数：
// 整型 → JSON number；true/false → JSON bool；其余保持字符串。
func heuristicValue(raw string) any {
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return value
	}
	if raw == "true" {
		return true
	}
	if raw == "false" {
		return false
	}
	return raw
}

// formValuesObject 把表单键值转换为可序列化对象（多值 → 数组）。
func formValuesObject(values url.Values) map[string]any {
	object := make(map[string]any, len(values))
	for key, list := range values {
		if len(list) == 1 {
			object[key] = heuristicValue(list[0])
			continue
		}
		items := make([]any, 0, len(list))
		for _, item := range list {
			items = append(items, heuristicValue(item))
		}
		object[key] = items
	}
	return object
}

// marshalJSON 序列化参数摘要；失败时回退为 "[params serialization failed]"。
func marshalJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "[params serialization failed]"
	}
	return string(encoded)
}

// truncateParams：112-115 的 2000 字符截断（按 Unicode 码点计）。
func truncateParams(text string) string {
	runes := []rune(text)
	if len(runes) <= domain.AuditLogRequestParamsMaxLength {
		return text
	}
	return string(runes[:domain.AuditLogRequestParamsMaxLength]) + domain.AuditLogRequestParamsTruncatedSuffix
}
