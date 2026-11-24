package browser

import (
	"errors"
	"slices"
	"testing"
)

func TestOpenerCommand(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		wsl       bool
		available []string
		wantName  string
		wantErr   error
	}{
		{"darwin", "darwin", false, nil, "open", nil},
		{"windows", "windows", false, nil, "rundll32", nil},
		{"wsl prefers wslview", "linux", true, []string{"wslview", "rundll32.exe", "xdg-open"}, "wslview", nil},
		{"wsl windows browser", "linux", true, []string{"rundll32.exe", "xdg-open"}, "rundll32.exe", nil},
		{"linux xdg-open", "linux", false, []string{"xdg-open"}, "xdg-open", nil},
		{"linux ignores windows", "linux", false, []string{"rundll32.exe"}, "", ErrNoOpener},
		{"linux nothing", "linux", false, nil, "", ErrNoOpener},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var started string
			o := &Opener{
				goos: tt.goos,
				wsl:  tt.wsl,
				lookPath: func(name string) (string, error) {
					if slices.Contains(tt.available, name) {
						return "/usr/bin/" + name, nil
					}
					return "", errors.New("not found")
				},
				start: func(name string, _ ...string) error { started = name; return nil },
			}
			err := o.Open("https://example.com")
			if !errors.Is(err, tt.wantErr) || started != tt.wantName {
				t.Errorf("Open() started %q, err %v; want %q, %v", started, err, tt.wantName, tt.wantErr)
			}
		})
	}
}
