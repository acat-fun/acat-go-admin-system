package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/acat-fun/acat-go-admin-system/auth/domain"
)

// seedWorker 写入一个带 bcrypt 密码的工作人员。
func seedWorker(t *testing.T, f *adminFixture, id, username, rawPassword string) {
	t.Helper()
	hashed, err := bcrypt.GenerateFromPassword([]byte(rawPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成密码哈希: %v", err)
	}
	f.workers.workers[id] = &domain.AdminWorker{
		ID: id, Username: username, Password: string(hashed), Status: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

func TestChangePasswordUpdatesHashAndRevokesSession(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	seedWorker(t, f, "u1", "self", "old-password-123")

	token, err := f.svc.satoken.Login(ctx, "u1")
	if err != nil {
		t.Fatalf("登录写入会话: %v", err)
	}

	if err := f.svc.ChangePassword(ctx, "u1", "old-password-123", "new-password-456"); err != nil {
		t.Fatalf("修改密码: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(f.workers.workers["u1"].Password), []byte("new-password-456")); err != nil {
		t.Fatalf("密码未更新为新密码: %v", err)
	}
	if _, err := f.svc.satoken.CheckLogin(ctx, token); err == nil {
		t.Fatal("改密后旧会话必须失效")
	}
}

func TestChangePasswordRejectsWrongOldPassword(t *testing.T) {
	f := newAdminFixture(t)
	seedWorker(t, f, "u1", "self", "old-password-123")

	err := f.svc.ChangePassword(context.Background(), "u1", "wrong-password-1", "new-password-456")
	if got := businessMessage(t, err); got != MessageOldPasswordMismatch {
		t.Fatalf("提示 = %q，期望 %q", got, MessageOldPasswordMismatch)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(f.workers.workers["u1"].Password), []byte("old-password-123")); err != nil {
		t.Fatal("原密码错误时不得改动密码")
	}
}

func TestChangePasswordValidatesInput(t *testing.T) {
	f := newAdminFixture(t)
	seedWorker(t, f, "u1", "self", "old-password-123")
	ctx := context.Background()

	if got := businessMessage(t, f.svc.ChangePassword(ctx, "u1", "", "new-password-456")); got != MessagePasswordRequired {
		t.Fatalf("空原密码提示 = %q", got)
	}
	if got := businessMessage(t, f.svc.ChangePassword(ctx, "u1", "old-password-123", "short")); !strings.Contains(got, "至少") {
		t.Fatalf("长度不足提示 = %q", got)
	}
	if got := businessMessage(t, f.svc.ChangePassword(ctx, "u1", "old-password-123", "old-password-123")); got != MessagePasswordUnchanged {
		t.Fatalf("新旧相同提示 = %q", got)
	}
}
