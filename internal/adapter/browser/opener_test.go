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
		available []string
		wantName  string
		wantErr   error
	}{
		{"darwin", "darwin", nil, "open", nil},
		{"windows", "windows", nil, "rundll32", nil},
		{"wsl prefers wslview", "linux", []string{"wslview", "xdg-open"}, "wslview", nil},
		{"linux xdg-open", "linux", []string{"xdg-open"}, "xdg-open", nil},
		{"linux nothing", "linux", nil, "", ErrNoOpener},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var started string
			o := &Opener{
				goos: tt.goos,
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
