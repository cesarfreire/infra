package domain

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// TaskDefinition is a revision of a task definition (immutable).
type TaskDefinition struct {
	Family     string
	Revision   int
	CPU        string
	Memory     string
	Containers []ContainerDefinition
}

// ContainerDefinition is a container of a task definition.
type ContainerDefinition struct {
	Name    string
	Image   string
	CPU     int
	Memory  int // hard limit in MiB; 0 when unset
	Env     map[string]string
	Secrets map[string]string // name → where the value comes from (ARN)
	Ports   []int
}

// Name returns "family:revision".
func (d TaskDefinition) Name() string { return d.Family + ":" + strconv.Itoa(d.Revision) }

// PreviousRevision returns "family:revision-1", or "" for the first one.
func (d TaskDefinition) PreviousRevision() string {
	if d.Revision <= 1 {
		return ""
	}
	return d.Family + ":" + strconv.Itoa(d.Revision-1)
}

// ParseTaskDefinitionName splits "family:revision".
func ParseTaskDefinitionName(name string) (family string, revision int, ok bool) {
	family, rev, found := strings.Cut(name, ":")
	if !found {
		return "", 0, false
	}
	revision, err := strconv.Atoi(rev)
	return family, revision, err == nil && family != ""
}

// Change is a difference between two task definitions.
type Change struct {
	Field string // e.g. "app.image", "app.env.LOG_LEVEL", "memory"
	Old   string // empty when added
	New   string // empty when removed
}

// DiffTaskDefinitions lists what changed from old to new, sorted by field.
// Secret values are never read; only where they come from is compared.
func DiffTaskDefinitions(old, new TaskDefinition) []Change {
	var changes []Change
	add := func(field, a, b string) {
		if a != b {
			changes = append(changes, Change{Field: field, Old: a, New: b})
		}
	}
	add("cpu", old.CPU, new.CPU)
	add("memory", old.Memory, new.Memory)

	containers := map[string][2]*ContainerDefinition{}
	for i := range old.Containers {
		c := &old.Containers[i]
		pair := containers[c.Name]
		pair[0] = c
		containers[c.Name] = pair
	}
	for i := range new.Containers {
		c := &new.Containers[i]
		pair := containers[c.Name]
		pair[1] = c
		containers[c.Name] = pair
	}
	for _, name := range slices.Sorted(maps.Keys(containers)) {
		a, b := containers[name][0], containers[name][1]
		switch {
		case a == nil:
			add(name, "", "added ("+b.Image+")")
			continue
		case b == nil:
			add(name, "removed ("+a.Image+")", "")
			continue
		}
		add(name+".image", a.Image, b.Image)
		add(name+".cpu", itoa(a.CPU), itoa(b.CPU))
		add(name+".memory", itoa(a.Memory), itoa(b.Memory))
		add(name+".ports", fmt.Sprint(a.Ports), fmt.Sprint(b.Ports))
		diffMaps(name+".env.", a.Env, b.Env, add)
		diffMaps(name+".secret.", a.Secrets, b.Secrets, add)
	}
	slices.SortStableFunc(changes, func(x, y Change) int { return strings.Compare(x.Field, y.Field) })
	return changes
}

func diffMaps(prefix string, a, b map[string]string, add func(field, a, b string)) {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		add(prefix+k, a[k], b[k])
	}
}

func itoa(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}
