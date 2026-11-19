//go:build windows

package mount

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Windows has no mount.cifs: shares are reached directly over UNC through the
// system SMB client. Authenticated access establishes a per-logon-session WNet
// connection; the password travels only through the in-memory WNet API call,
// never a command line or file. Writable is ignored: access rights are whatever
// the share grants the authenticated user.

type netResourceW struct {
	scope, typ, displayType, usage uint32
	localName, remoteName          *uint16
	comment, provider              *uint16
}

var (
	mpr                   = syscall.NewLazyDLL("mpr.dll")
	procWNetAddConnection = mpr.NewProc("WNetAddConnection2W")
	procWNetCancelConnect = mpr.NewProc("WNetCancelConnection2W")
)

const (
	resTypeAny        = 0
	connectTemporary  = 0x00000004
	wnSuccess         = 0
	wnAccessDenied    = 5
	wnBadNetName      = 67
	wnNetPathNotFound = 53
	wnAlreadyAssigned = 85
	wnInvalidPassword = 86
	wnCredConflict    = 1219
	wnNoNetwork       = 1222
	wnNotConnected    = 2250
)

type winBackend struct{}

func newPlatformBackend(_ Runner, _ string) Backend { return winBackend{} }

func winError(code uintptr, unc string) error {
	switch code {
	case wnAccessDenied:
		return fmt.Errorf("访问 %s 被拒绝：请检查用户名/密码与共享权限", unc)
	case wnInvalidPassword:
		return fmt.Errorf("访问 %s 密码错误", unc)
	case wnBadNetName, wnNetPathNotFound:
		return fmt.Errorf("找不到共享 %s：请检查主机与共享名", unc)
	case wnCredConflict:
		return fmt.Errorf("已存在到该服务器的其他凭据会话：请先卸载同主机其他共享，或改用相同凭据")
	case wnNoNetwork:
		return fmt.Errorf("网络不可用：无法连接 %s", unc)
	default:
		return fmt.Errorf("连接 %s 失败（Windows 错误 %d）", unc, code)
	}
}

func (winBackend) Mount(ctx context.Context, s Spec) error {
	if err := s.Validate(); err != nil {
		return err
	}
	ch := make(chan error, 1)
	go func() { ch <- winConnect(s) }()
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return fmt.Errorf("连接 %s 超时", s.UNC())
	}
}

// nativeUNC renders the backslash form WNet expects; the rest of the server
// keeps the forward-slash UNC that Go's file APIs accept on Windows.
func nativeUNC(unc string) string { return strings.ReplaceAll(unc, "/", `\`) }

func winConnect(s Spec) error {
	unc := s.UNC()
	native := nativeUNC(unc)
	if s.NeedsCredentials() {
		user := s.Username
		if s.Domain != "" {
			user = s.Domain + "\\" + s.Username
		}
		nr := netResourceW{typ: resTypeAny, remoteName: syscall.StringToUTF16Ptr(native)}
		add := func() uintptr {
			r, _, _ := procWNetAddConnection.Call(
				uintptr(unsafe.Pointer(&nr)),
				uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(s.Password))),
				uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(user))),
				connectTemporary)
			return r
		}
		r := add()
		if r == wnAlreadyAssigned || r == wnCredConflict {
			// A session to this server already exists (possibly guest or other
			// credentials). Drop it and retry once with the requested credentials.
			procWNetCancelConnect.Call(uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(native))), 0, 1)
			r = add()
		}
		if r != wnSuccess {
			return winError(r, unc)
		}
	}
	if _, err := os.Stat(unc); err != nil {
		if s.NeedsCredentials() {
			return fmt.Errorf("访问 %s 失败：%w（用户名或密码不正确，或共享权限不足）", unc, err)
		}
		return fmt.Errorf("访问 %s 失败：%w（共享需要认证时请填写用户名与密码）", unc, err)
	}
	return nil
}

func (winBackend) Unmount(_ context.Context, target string) error {
	if err := validateTarget(target); err != nil {
		return err
	}
	r, _, _ := procWNetCancelConnect.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(nativeUNC(target)))), 0, 1)
	if r != wnSuccess && r != wnNotConnected {
		return winError(r, target)
	}
	return nil
}
