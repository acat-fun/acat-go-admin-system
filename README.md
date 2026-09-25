# acat-go-admin-system — 管理端系统管理域共享 Go 模块

> 管理端「系统管理」七域（字典/页面/i18n/前端模块/文件/审计日志/通知）与权限判定的共享实现，
> 抽取自 acat-admin-system / acat-admin-user（权威源），供 acat 管理栈与 devops 平台共用。

## 模块信息

- module path：`github.com/acat-fun/acat-go-admin-system`（权威源 Gitea，Push Mirror 到 GitHub）
- 依赖：`github.com/acat-fun/acat-go-common`（db/result/apperr/satoken/middleware/permission）、mongo-driver v2（审计 Mongo 存储）

## 包结构

| 包 | 职责 |
| --- | --- |
| `domain` | 七域领域模型（dict/page/i18n/frontend_module/file/auditlog）与审计字段填充 |
| `repo` | MySQL 数据访问（`t_acat_*` 表）+ Mongo 审计存储（`audit_logs` 集合） |
| `service` | 七域业务逻辑（事务边界只在本层） |
| `httpapi` | HTTP 处理器与路由注册（`/api/admin/system/**` 契约） |
| `logic` | 权限码常量 + Checker/Actor（超管按角色：会话 roles 含 root） |
| `storage` | 对象存储接口 + 内存实现 + S3/MinIO（SigV4）实现 |
| `audit` | 审计写入器（Recorder）与路由 detail 映射 |
| `notification` | 站内通知域（`t_acat_notification`，U10-U12 契约：列表/未读数/已读） |

## 数据契约

- 表：`acat_user` 库 `t_acat_dict`、`t_acat_dict_data`、`t_acat_i18n_type`、`t_acat_i18n_label`、
  `t_acat_page`、`t_acat_page_history`、`t_acat_frontend_module`、`t_acat_file`、`t_acat_notification`；
  审计日志存 MongoDB `audit_logs`（SQL 基线见 acat-db-baseline 仓）。
- 超级管理员：**按角色判定**——会话 roles 快照含 `root`（`t_acat_role.id = code = "root"`），
  判定入口 `logic.IsRootSession` / `logic.Actor.IsRoot`（与 acat-go-common/permission 同口径）。

## 消费方式

服务仓把本模块作为依赖引入，装配形态参考 acat-admin-system 的 `cmd/main.go`：

```go
import (
    adminrepo "github.com/acat-fun/acat-go-admin-system/repo"
    adminsvc "github.com/acat-fun/acat-go-admin-system/service"
    adminapi "github.com/acat-fun/acat-go-admin-system/httpapi"
)

svc, err := adminsvc.New(adminsvc.Options{Dicts: adminrepo.NewDictRepo(dbHandle), ...})
api, err := adminapi.New(adminapi.Options{Service: svc, Satoken: logic, ...})
api.Register(mux)
```

通知域（各端挂自己的路由前缀）：

```go
notifSvc, _ := notification.New(notification.Options{Repo: notification.NewMySQLRepo(dbHandle, notification.RepoOptions{})})
h := notification.NewHandler(notifSvc)
mux.Handle("GET /api/.../notifications", auth(http.HandlerFunc(h.HandleList)))
mux.HandleFunc("PATCH /api/.../notifications/{id}", func(w, r) { h.HandleMarkRead(w, r, r.PathValue("id")) })
mux.Handle("PATCH /api/.../notifications", auth(http.HandlerFunc(h.HandleMarkAllRead)))
```

## 验证

```bash
gofmt -l . && go vet ./... && go test ./...
```

## 发布

版本固定 `v0.x.y` 语义 tag；本地联调期 `go.mod` 中的 `replace => ../acat-go-common` 在发布前移除
（依赖 acat-go-common ≥ v0.3.0）。
