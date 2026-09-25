module 47.108.230.93/acat-fun/acat-go-admin-system

go 1.25.0

require (
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/acat-fun/acat-go-common v0.3.0
	go.mongodb.org/mongo-driver/v2 v2.5.0
	golang.org/x/crypto v0.33.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-sql-driver/mysql v1.9.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.17.6 // indirect
	github.com/redis/go-redis/v9 v9.22.0 // indirect
	github.com/xdg-go/scram v1.2.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sync v0.11.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/text v0.22.0 // indirect
)

// 本地开发联调：发布前改为远端 require（go-common 发版 v0.3.0 后删除本 replace）。
replace github.com/acat-fun/acat-go-common => ../acat-go-common
