package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// ID 生成：RFC 9562 UUID v7（时间有序）。
//
// 生成 36 字符小写带连字符的字符串，可直接写入 VARCHAR(36) 主键列。
// 布局：timestamp_ms(48) | version=0x7(4) | rand_a(12) | variant=0b10(2) | rand_b(62)。

// IDByteLength 是 UUID 的字节长度。
const IDByteLength = 16

// NewID 生成一个新的 UUID v7 字符串（36 字符、小写、带连字符）。
//
// 时间戳取当前毫秒，保证按 id 排序即时间有序；同毫秒内由随机位保证唯一。
func NewID() string {
	return NewIDAt(time.Now())
}

// NewIDAt 以指定时间生成 UUID v7，便于测试断言时间戳字段。
func NewIDAt(now time.Time) string {
	var raw [IDByteLength]byte

	// 48bit 毫秒时间戳（大端）。
	millis := uint64(now.UnixMilli())
	raw[0] = byte(millis >> 40)
	raw[1] = byte(millis >> 32)
	raw[2] = byte(millis >> 24)
	raw[3] = byte(millis >> 16)
	raw[4] = byte(millis >> 8)
	raw[5] = byte(millis)

	// crypto/rand.Read 自 Go 1.24 起保证不返回错误（内部不可恢复失败会直接终止进程），
	// 因此这里不引入永不触发的错误分支，避免污染 NewID 的调用签名。
	_, _ = rand.Read(raw[6:])

	raw[6] = (raw[6] & 0x0f) | 0x70 // version 7
	raw[8] = (raw[8] & 0x3f) | 0x80 // variant 0b10

	return formatUUID(raw)
}

// formatUUID 输出 8-4-4-4-12 小写十六进制形式。
func formatUUID(raw [IDByteLength]byte) string {
	var buf [36]byte
	hex.Encode(buf[0:8], raw[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], raw[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], raw[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], raw[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], raw[10:16])
	return string(buf[:])
}
