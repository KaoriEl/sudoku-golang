// Package notify shows desktop notifications on macOS (osascript), Linux (notify-send) and Windows
// (a WinRT toast through PowerShell). Title and body are passed as arguments / environment, never
// spliced into a script, so container names cannot inject anything.
package notify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sudoku-golang/internal/sysproc"
	"time"
)

var ErrUnsupported = errors.New("desktop notifications are not available")

const sendTimeout = 10 * time.Second

const appleScript = `on run argv
display notification (item 2 of argv) with title (item 1 of argv)
end run`

// The AppUserModelID of Windows PowerShell is registered on every Windows 10/11, so the toast shows
// without installing a shortcut for sudoku.
const winScript = `$ErrorActionPreference = 'Stop'
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$t = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$x = $t.GetElementsByTagName('text')
$x.Item(0).AppendChild($t.CreateTextNode($env:SUDOKU_TITLE)) > $null
$x.Item(1).AppendChild($t.CreateTextNode($env:SUDOKU_BODY)) > $null
$n = [Windows.UI.Notifications.ToastNotification]::new($t)
$id = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($id).Show($n)`

func command(ctx context.Context, title, body string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "osascript", "-e", appleScript, title, body), nil
	case "windows":
		cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", winScript)
		cmd.Env = append(os.Environ(), "SUDOKU_TITLE="+title, "SUDOKU_BODY="+body)
		return cmd, nil
	case "linux", "freebsd", "openbsd", "netbsd":
		return exec.CommandContext(ctx, "notify-send", "--app-name=sudoku", "--", title, body), nil
	}
	return nil, ErrUnsupported
}

// Supported tells whether Send can work here and, if not, why.
func Supported() (bool, string) {
	bin := map[string]string{"darwin": "osascript", "windows": "powershell.exe"}[runtime.GOOS]
	if bin == "" {
		bin = "notify-send"
	}
	if _, err := exec.LookPath(bin); err != nil {
		if bin == "notify-send" {
			return false, "не найден notify-send (пакет libnotify-bin / libnotify)"
		}
		return false, bin + " не найден"
	}
	return true, ""
}

func clean(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return strings.TrimSpace(s)
}

// Send shows one notification and waits for the helper to finish.
func Send(ctx context.Context, title, body string) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	cmd, err := command(ctx, clean(title, 120), clean(body, 400))
	if err != nil {
		return err
	}
	sysproc.HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("notify: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
