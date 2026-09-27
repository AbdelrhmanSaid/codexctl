package update

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

const (
	detachedProcess = 0x00000008
	createNoWindow  = 0x08000000
)

// Remove renames the file, since Windows cannot delete a running executable;
// a detached cmd.exe deletes it later.
func Remove(exe string) error {
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// cmd strips the outer quotes of a /c argument that starts with one.
		CmdLine:       fmt.Sprintf(`cmd.exe /d /c "ping -n 3 127.0.0.1 >nul & del /f /q "%s""`, old),
		CreationFlags: detachedProcess | createNoWindow,
		HideWindow:    true,
	}
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
	}
	return nil
}
