package instances

import (
	"time"

	"infra/internal/domain"
	"infra/internal/filter"
	"infra/internal/tui/components/details"
	"infra/internal/tui/components/listview"
)

const filterPlaceholder = "name, state:running, type:t3, ip:10.0, az:1a, tag:env=prod"

var columns = []listview.Column[domain.Instance]{
	{Title: "Name", Value: func(i domain.Instance) string { return i.Name }},
	{Title: "ID", Width: 19, Value: func(i domain.Instance) string { return i.ID }},
	{Title: "State", Width: 13, Value: func(i domain.Instance) string { return string(i.State) }},
	{Title: "Type", Width: 12, Value: func(i domain.Instance) string { return i.Type }},
	{Title: "Private IP", Width: 15, Value: func(i domain.Instance) string { return i.PrivateIP }},
	{Title: "AZ", Width: 12, Value: func(i domain.Instance) string { return i.AvailabilityZone }},
}

// filterFields exposes the instance attributes to the filter language.
func filterFields(inst domain.Instance) []filter.Field {
	fields := []filter.Field{
		{Name: "name", Value: inst.Name},
		{Name: "id", Value: inst.ID},
		{Name: "state", Value: string(inst.State)},
		{Name: "type", Value: inst.Type},
		{Name: "ip", Value: inst.PrivateIP},
		{Name: "ip", Value: inst.PublicIP},
		{Name: "az", Value: inst.AvailabilityZone},
		{Name: "platform", Value: inst.Platform},
	}
	for k, v := range inst.Tags {
		fields = append(fields, filter.Field{Name: "tag", Value: k + "=" + v})
	}
	return fields
}

func (m *Model) View() string {
	inst := m.selected
	switch m.view {
	case viewActions:
		return m.menu.View(m.theme, inst.DisplayName(), inst.ID+" · "+string(inst.State))
	case viewDetails:
		return m.details.View()
	case viewPortForward:
		return m.form.View(m.theme, "Port forward", inst.DisplayName()+" · "+inst.ID)
	}
	return m.list.View()
}

func (m *Model) detailsHeader(inst domain.Instance) string {
	launched := ""
	if !inst.LaunchTime.IsZero() {
		launched = inst.LaunchTime.Local().Format(time.DateTime)
	}
	return details.Render(m.theme, inst.DisplayName(), "details", []details.Field{
		{Label: "ID", Value: inst.ID},
		{Label: "Name", Value: inst.Name},
		{Label: "State", Value: string(inst.State)},
		{Label: "Type", Value: inst.Type},
		{Label: "Private IP", Value: inst.PrivateIP},
		{Label: "Public IP", Value: inst.PublicIP},
		{Label: "AZ", Value: inst.AvailabilityZone},
		{Label: "Platform", Value: inst.Platform},
		{Label: "Launched", Value: launched},
	}, details.Section{Title: "Tags", Fields: details.Tags(inst.Tags)})
}
