// Package browser opens URLs in the user's default browser.
package browser

import (
	"errors"
	"os/exec"
	"runtime"
)

// ErrNoOpener is returned when no browser launcher is available.
var ErrNoOpener = errors.New("no browser launcher found")

// Opener implements app.URLOpener using the OS launcher.
type Opener struct {
	goos     string
	lookPath func(string) (string, error)
	start    func(name string, args ...string) error
}

// New returns an Opener for the current OS.
func New() *Opener {
	return &Opener{goos: runtime.GOOS, lookPath: exec.LookPath, start: startDetached}
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
	// wslview comes first so WSL opens the Windows browser.
	for _, name := range []string{"wslview", "xdg-open"} {
		if _, err := o.lookPath(name); err == nil {
			return name, []string{url}, nil
		}
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
