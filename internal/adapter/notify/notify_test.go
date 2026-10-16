package notify

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func desktop(goos string, wsl bool, available ...string) (*Desktop, *[]string) {
	var started []string
	return &Desktop{
		goos: goos,
		wsl:  wsl,
		lookPath: func(name string) (string, error) {
			if slices.Contains(available, name) {
				return "/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		start: func(name string, args ...string) error {
			started = append(started, name+" "+strings.Join(args, " "))
			return nil
		},
	}, &started
}

func TestPlatforms(t *testing.T) {
	tests := []struct {
		name      string
		d         *Desktop
		started   *[]string
		wantStart string
	}{}
	add := func(name, want string, d *Desktop, started *[]string) {
		tests = append(tests, struct {
			name      string
			d         *Desktop
			started   *[]string
			wantStart string
		}{name, d, started, want})
	}
	d, s := desktop("darwin", false)
	add("macOS", "osascript", d, s)
	d, s = desktop("linux", true, "powershell.exe", "notify-send")
	add("WSL prefers the Windows toast", "powershell.exe", d, s)
	d, s = desktop("linux", false, "notify-send")
	add("Linux", "notify-send", d, s)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.d.Notify("Deployment of api", "completed in 3m"); err != nil {
				t.Fatal(err)
			}
			if got := (*tt.started)[0]; !strings.HasPrefix(got, tt.wantStart) || !strings.Contains(got, "completed in 3m") {
				t.Errorf("started %q", got)
			}
		})
	}
}

func TestUnavailable(t *testing.T) {
	d, _ := desktop("linux", false)
	if d.Available() || !errors.Is(d.Notify("t", "b"), ErrUnavailable) {
		t.Error("without a command, notifications are unavailable")
	}
}

func TestQuoting(t *testing.T) {
	if got := psString("it's"); got != "'it''s'" {
		t.Errorf("psString = %s", got)
	}
	if got := appleString(`say "hi" \ bye`); got != `"say \"hi\" \\ bye"` {
		t.Errorf("appleString = %s", got)
	}
	d, s := desktop("linux", true, "powershell.exe")
	d.Notify("api's deploy", "done")
	if !strings.Contains((*s)[0], "'api''s deploy'") {
		t.Errorf("titles must be quoted for PowerShell: %s", (*s)[0])
	}
}
