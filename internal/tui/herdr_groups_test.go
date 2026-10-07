package tui

import (
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	"testing"
)

func TestHostGroupsAreNonActionableAndTagsSearchable(t *testing.T) {
	l := NewListView(newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()}))
	endpoints := []herdrhub.Endpoint{{ID: "u", Label: "Ungrouped", Target: "u"}, {ID: "z", Label: "Zulu host", Target: "z", Group: "Zulu"}, {ID: "a", Label: "Alpha host", Target: "a", Group: "Alpha", Tags: []string{"linux-special-tag"}}}
	l.SetHerdrHubSnapshots(endpoints, nil)
	order := []string{}
	groups := []string{}
	for _, item := range l.items {
		if item.Endpoint != nil && item.Endpoint.ID != herdrhub.LocalEndpointID {
			order = append(order, item.Endpoint.ID)
		}
		if item.Type == "remote_header" {
			groups = append(groups, item.Name)
			if !listItemIsHeader(item) {
				t.Fatal("group is actionable")
			}
		}
	}
	if len(order) != 3 || order[0] != "u" || order[1] != "a" || order[2] != "z" {
		t.Fatalf("order=%v", order)
	}
	if len(groups) != 2 {
		t.Fatalf("groups=%v", groups)
	}
	typeListQuery(l, "linux-special-tag")
	if s := l.Selected(); s == nil || s.Endpoint == nil || s.Endpoint.ID != "a" {
		t.Fatalf("tag missing: %+v", s)
	}
}
