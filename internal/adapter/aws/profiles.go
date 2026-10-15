package aws

import (
	"bufio"
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"infra/internal/domain"
)

// ProfileStore reads profiles from the AWS CLI config and credentials files.
type ProfileStore struct {
	configPath      string
	credentialsPath string
}

// NewProfileStore uses the same locations as the AWS CLI, honoring
// AWS_CONFIG_FILE and AWS_SHARED_CREDENTIALS_FILE.
func NewProfileStore() *ProfileStore {
	home, _ := os.UserHomeDir()
	return &ProfileStore{
		configPath:      cmp.Or(os.Getenv("AWS_CONFIG_FILE"), filepath.Join(home, ".aws", "config")),
		credentialsPath: cmp.Or(os.Getenv("AWS_SHARED_CREDENTIALS_FILE"), filepath.Join(home, ".aws", "credentials")),
	}
}

// List returns every profile, "default" first and the rest by name. Files
// are read on each call so edits are picked up without restarting.
func (s *ProfileStore) List() ([]domain.Profile, error) {
	byName := map[string]*domain.Profile{}
	get := func(name string) *domain.Profile {
		if p, ok := byName[name]; ok {
			return p
		}
		p := &domain.Profile{Name: name}
		byName[name] = p
		return p
	}

	// In the config file sections are "[default]" and "[profile name]";
	// "[sso-session x]" and "[services x]" are not profiles.
	err := readINI(s.configPath, func(section, key, value string) {
		name, ok := configProfileName(section)
		if !ok {
			return
		}
		p := get(name)
		switch key {
		case "region":
			p.Region = value
		case "sso_session", "sso_start_url":
			p.SSO = true
		}
	})
	if err != nil {
		return nil, err
	}
	// In the credentials file every section is a profile name.
	if err := readINI(s.credentialsPath, func(section, _, _ string) { get(section) }); err != nil {
		return nil, err
	}

	profiles := make([]domain.Profile, 0, len(byName))
	for _, p := range byName {
		profiles = append(profiles, *p)
	}
	slices.SortFunc(profiles, func(a, b domain.Profile) int {
		if (a.Name == "default") != (b.Name == "default") {
			if a.Name == "default" {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return profiles, nil
}

func configProfileName(section string) (string, bool) {
	if section == "default" {
		return section, true
	}
	name, ok := strings.CutPrefix(section, "profile ")
	return strings.TrimSpace(name), ok
}

// readINI calls fn for every key of every section; a section without keys
// is reported once with an empty key. A missing file is not an error.
func readINI(path string, fn func(section, key, value string)) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	section := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "", line[0] == '#', line[0] == ';':
		case line[0] == '[' && strings.HasSuffix(line, "]"):
			section = strings.TrimSpace(line[1 : len(line)-1])
			fn(section, "", "")
		case section != "":
			if k, v, ok := strings.Cut(line, "="); ok {
				fn(section, strings.TrimSpace(k), strings.TrimSpace(v))
			}
		}
	}
	return scanner.Err()
}
