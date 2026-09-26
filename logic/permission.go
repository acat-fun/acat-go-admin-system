package logic

import (
	"context"
	"fmt"

	"github.com/acat-fun/acat-go-admin-system/domain"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/satoken"
)

// Checker 封装管理端权限判定：root 角色直接放行，其余读 Sa-Token 会话权限快照。
type Checker struct {
	logic *satoken.Logic
}

// NewChecker 构造 Checker；logic 为 nil 时任何非 root 判定都会拒绝（不 panic）。
func NewChecker(logic *satoken.Logic) *Checker { return &Checker{logic: logic} }

// CheckPermission 判定会话是否具备权限码 code。
//
// 判定口径：
//   - 会话 roles 快照含 root（超管角色）直接放行（不查库、不看会话权限）；
//   - 其余用户使用 Logic.CheckPermission 读会话 permissions 快照；
//   - 不通过返回 apperr.Forbidden("无操作权限")（HTTP 403）。
func (c *Checker) CheckPermission(session *satoken.Session, code string) error {
	if IsRootSession(session) {
		return nil
	}
	if c == nil || c.logic == nil || session == nil || code == "" {
		return apperr.Forbidden(MessageForbidden)
	}
	if !c.logic.CheckPermission(session, code) {
		return apperr.Forbidden(MessageForbidden)
	}
	return nil
}

// CheckAnyPermission 判定会话是否具备 codes 中任意一个权限码（OR 语义）。
//
// root 角色直接放行；codes 为空视为无需权限。
func (c *Checker) CheckAnyPermission(session *satoken.Session, codes ...string) error {
	if len(codes) == 0 {
		return nil
	}
	if IsRootSession(session) {
		return nil
	}
	for _, code := range codes {
		if c == nil || c.logic == nil || session == nil || code == "" {
			continue
		}
		if c.logic.CheckPermission(session, code) {
			return nil
		}
	}
	return apperr.Forbidden(MessageForbidden)
}

// LoginID 从会话读取登录 id；会话为空或字段缺失时返回空串。
func LoginID(session *satoken.Session) string {
	if session == nil || session.LoginID == nil {
		return ""
	}
	if value, ok := session.LoginID.(string); ok {
		return value
	}
	return fmt.Sprintf("%v", session.LoginID)
}

// IsRootSession 判断会话是否绑定超级管理员角色（roles 快照含 root）。
func IsRootSession(session *satoken.Session) bool {
	if session == nil {
		return false
	}
	return domain.IsRootRole(session.StringList(satoken.DataKeyRoles))
}

// Actor 是当前请求的操作者视图（登录 id + 会话 + 权限判定器）。
//
// 等价于请求上下文：Service 层用它完成 isRoot / 权限码判定。
type Actor struct {
	// LoginID 当前登录用户 id；未登录为空串。
	LoginID string
	// Session 当前账号会话（可能为 nil）。
	Session *satoken.Session
	checker *Checker
}

// ActorFrom 从请求上下文（middleware.Auth 写入的会话）构造操作者。
func ActorFrom(ctx context.Context, checker *Checker) *Actor {
	session := middleware.SessionFrom(ctx)
	return &Actor{LoginID: LoginID(session), Session: session, checker: checker}
}

// IsRoot 判断当前操作者是否为超级管理员（会话角色含 root）。
func (a *Actor) IsRoot() bool {
	return a != nil && IsRootSession(a.Session)
}

// RequirePermission 判定权限码：root 角色放行，其余按会话权限判定，不通过返回 403。
func (a *Actor) RequirePermission(code string) error {
	if a == nil {
		return apperr.Forbidden(MessageForbidden)
	}
	if a.IsRoot() {
		return nil
	}
	if a.checker == nil {
		return apperr.Forbidden(MessageForbidden)
	}
	return a.checker.CheckPermission(a.Session, code)
}

// RequireAnyPermission 判定权限码集合（OR 语义）：root 角色放行，空集合视为无需权限。
func (a *Actor) RequireAnyPermission(codes ...string) error {
	if len(codes) == 0 {
		return nil
	}
	if a == nil {
		return apperr.Forbidden(MessageForbidden)
	}
	if a.IsRoot() {
		return nil
	}
	if a.checker == nil {
		return apperr.Forbidden(MessageForbidden)
	}
	return a.checker.CheckAnyPermission(a.Session, codes...)
}

// RequirePermissionOrSelf 自操作绕过：当前登录 id == 目标 id 时直接放行，
// 否则要求 code 权限。
func (a *Actor) RequirePermissionOrSelf(targetID, code string) error {
	if a != nil && targetID != "" && a.LoginID == targetID {
		return nil
	}
	return a.RequirePermission(code)
}
