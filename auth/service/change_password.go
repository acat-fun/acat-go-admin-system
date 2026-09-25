package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"47.108.230.93/acat-fun/acat-go-admin-system/auth/repo"
	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/satoken"
)

// 修改密码相关文案与约束。
const (
	// MessagePasswordRequired 原密码与新密码都必须填写。
	MessagePasswordRequired = "请输入原密码与新密码"
	// MessagePasswordTooShort 新密码长度不足（%d 为最小长度）。
	MessagePasswordTooShort = "新密码至少 %d 位"
	// MessagePasswordUnchanged 新密码不能与原密码相同。
	MessagePasswordUnchanged = "新密码不能与原密码相同"
	// MessageOldPasswordMismatch 原密码不正确。
	MessageOldPasswordMismatch = "原密码不正确"
	// MessagePasswordUserMissing 会话用户已不存在。
	MessagePasswordUserMissing = "用户不存在"
	// MinPasswordLength 新密码最小长度。
	MinPasswordLength = 12
)

// ChangePassword 修改本人登录密码。
//
// 流程：校验输入 → 校验原密码 → 写入新密码 → 失效该账号当前的登录会话。
// 会话失效后调用方需要重新登录（前端改密成功后回到登录页）。
func (s *Service) ChangePassword(ctx context.Context, loginID, oldPassword, newPassword string) error {
	if s.adminWorkerRepo == nil {
		return fmt.Errorf("service: 工作人员数据访问未注入")
	}
	if strings.TrimSpace(oldPassword) == "" || strings.TrimSpace(newPassword) == "" {
		return apperr.NewBusiness(MessagePasswordRequired)
	}
	if utf8.RuneCountInString(newPassword) < MinPasswordLength {
		return apperr.NewBusiness(fmt.Sprintf(MessagePasswordTooShort, MinPasswordLength))
	}
	if oldPassword == newPassword {
		return apperr.NewBusiness(MessagePasswordUnchanged)
	}
	worker, err := s.adminWorkerRepo.FindByID(ctx, loginID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return apperr.NewBusiness(MessagePasswordUserMissing)
		}
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(worker.Password), []byte(oldPassword)); err != nil {
		return apperr.NewBusiness(MessageOldPasswordMismatch)
	}
	hashed, err := hashWorkerPassword(newPassword)
	if err != nil {
		return err
	}
	worker.Password = hashed
	worker.UpdatedAt = time.Now()
	affected, err := s.adminWorkerRepo.Update(ctx, worker)
	if err != nil {
		return err
	}
	s.warnIfNotUpdated(writeFact("update", TableAdminWorker, worker.ID, affected))
	if err := s.revokeLoginSessions(ctx, worker.ID); err != nil {
		s.logger.Error("修改密码后失效登录会话失败", "worker_id", worker.ID, "err", err)
		return err
	}
	return nil
}

// revokeLoginSessions 失效该账号当前的登录会话。
//
// 同一账号只保留最近一次登录的 token（登录时顶掉旧 token），因此注销该 token
// 即等于让此前的登录态全部失效。
func (s *Service) revokeLoginSessions(ctx context.Context, loginID string) error {
	token, err := s.satoken.TokenValueByLoginID(ctx, loginID)
	if err != nil {
		if errors.Is(err, satoken.ErrNotFound) {
			return nil
		}
		return err
	}
	if token == "" {
		return nil
	}
	return s.satoken.Logout(ctx, token)
}
