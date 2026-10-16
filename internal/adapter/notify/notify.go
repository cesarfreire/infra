// Package notify shows desktop notifications through the OS: notify-send
// on Linux, osascript on macOS and a Windows toast (via powershell.exe)
// under WSL.
package notify

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrUnavailable is returned when no notification command exists.
var ErrUnavailable = errors.New("no desktop notification command available")

// powershellAppID is the AppUserModelID of Windows PowerShell; toasts need
// a registered one to be shown.
const powershellAppID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

// Desktop sends notifications. Its zero value is not usable; use New.
type Desktop struct {
	goos     string
	wsl      bool
	lookPath func(string) (string, error)
	start    func(name string, args ...string) error
}

// New detects the platform (including WSL).
func New() *Desktop {
	return &Desktop{
		goos:     runtime.GOOS,
		wsl:      os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "",
		lookPath: exec.LookPath,
		start:    startDetached,
	}
}

// Available reports whether Notify can show anything.
func (d *Desktop) Available() bool {
	_, _, err := d.command("", "")
	return err == nil
}

// Notify shows a notification without waiting for it.
func (d *Desktop) Notify(title, body string) error {
	name, args, err := d.command(title, body)
	if err != nil {
		return err
	}
	return d.start(name, args...)
}

func (d *Desktop) command(title, body string) (string, []string, error) {
	switch {
	case d.goos == "darwin":
		script := "display notification " + appleString(body) + " with title " + appleString(title)
		return "osascript", []string{"-e", script}, nil
	case d.wsl && d.has("powershell.exe"):
		return "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", toastScript(title, body)}, nil
	case d.has("notify-send"):
		return "notify-send", []string{"--app-name=infra", title, body}, nil
	}
	return "", nil, ErrUnavailable
}

func (d *Desktop) has(name string) bool {
	_, err := d.lookPath(name)
	return err == nil
}

// appleString quotes s for AppleScript.
func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// psString quotes s for PowerShell (single quotes are doubled).
func psString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func toastScript(title, body string) string {
	return strings.Join([]string{
		"[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null",
		"$t = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)",
		"$x = $t.GetElementsByTagName('text')",
		"$x.Item(0).AppendChild($t.CreateTextNode(" + psString(title) + ")) > $null",
		"$x.Item(1).AppendChild($t.CreateTextNode(" + psString(body) + ")) > $null",
		"[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier(" + psString(powershellAppID) +
			").Show([Windows.UI.Notifications.ToastNotification]::new($t))",
	}, "; ")
}

func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
