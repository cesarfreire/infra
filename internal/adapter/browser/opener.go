// Package browser opens URLs in the user's default browser.
package browser

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// ErrNoOpener is returned when no browser launcher is available.
var ErrNoOpener = errors.New("no browser launcher found")

// Opener implements app.URLOpener using the OS launcher.
type Opener struct {
	goos     string
	wsl      bool
	lookPath func(string) (string, error)
	start    func(name string, args ...string) error
}

// New returns an Opener for the current OS.
func New() *Opener {
	return &Opener{
		goos:     runtime.GOOS,
		wsl:      os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "",
		lookPath: exec.LookPath,
		start:    startDetached,
	}
}

// Open launches the browser without waiting for it to exit.
func (o *Opener) Open(url string) error {
	name, args, err := o.command(url)
	if err != nil {
		return err
	}
	return o.start(name, args...)
}

func (o *Opener) command(url string) (string, []string, error) {
	switch o.goos {
	case "darwin":
		return "open", []string{url}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, nil
	}
	// Under WSL, open Windows' default browser; xdg-open would start a Linux
	// one in a separate WSLg window.
	if _, err := o.lookPath("wslview"); o.wsl && err == nil {
		return "wslview", []string{url}, nil
	}
	if _, err := o.lookPath("rundll32.exe"); o.wsl && err == nil {
		return "rundll32.exe", []string{"url.dll,FileProtocolHandler", url}, nil
	}
	if _, err := o.lookPath("xdg-open"); err == nil {
		return "xdg-open", []string{url}, nil
	}
	return "", nil, ErrNoOpener
}

func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
