/*
Copyright 2024 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mappingreconcile

import (
	"reflect"
	"testing"
)

func TestMatchesKeepsOriginalIdentificationRule(t *testing.T) {
	cases := map[string]struct {
		a, b Entry
		want bool
	}{
		"by id":                             {Entry{ID: "1"}, Entry{ID: "1"}, true},
		"by name":                           {Entry{Name: "admin"}, Entry{Name: "admin"}, true},
		"id matches despite differing name": {Entry{ID: "1", Name: "admin"}, Entry{ID: "1", Name: "other"}, true},
		"either field is enough":            {Entry{ID: "1", Name: "admin"}, Entry{ID: "9", Name: "admin"}, true},
		"unrelated":                         {Entry{ID: "1"}, Entry{ID: "2"}, false},
		"empty never matches":               {Entry{}, Entry{ID: "1", Name: "admin"}, false},
		"empty name does not match empty":   {Entry{Name: ""}, Entry{Name: ""}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.a.Matches(tc.b); got != tc.want {
				t.Errorf("Matches() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUpToDateIgnoresUnownedEntries(t *testing.T) {
	declared := []Entry{{Name: "admin"}}

	if !UpToDate(declared, []Entry{{Name: "admin"}}) {
		t.Error("declared entry present must be up to date")
	}
	if !UpToDate(declared, []Entry{{Name: "admin"}, {Name: "backups"}}) {
		t.Error("an extra unowned entry must not make the resource out of date")
	}
	if UpToDate(declared, []Entry{{Name: "backups"}}) {
		t.Error("a declared entry missing from the set must be out of date")
	}
	if !UpToDate(nil, nil) {
		t.Error("declaring nothing and owning nothing is trivially up to date")
	}
	if !UpToDate(nil, []Entry{{Name: "admin"}}) {
		t.Error("declaring nothing must not fight entries this resource does not own")
	}
}

func TestPlanAddsAndRemovesOnlyOwned(t *testing.T) {
	declared := []Entry{{Name: "admin"}}
	owned := []Entry{{Name: "admin"}, {Name: "stale"}}
	current := []Entry{{Name: "admin"}, {Name: "stale"}, {Name: "sibling"}}

	add, remove := Plan(declared, owned, current)
	if len(add) != 0 {
		t.Errorf("nothing to add, got %v", add)
	}
	if !reflect.DeepEqual(remove, []Entry{{Name: "stale"}}) {
		t.Errorf("remove = %v, want only the no-longer-declared owned entry", remove)
	}
}

func TestPlanNeverRemovesUnowned(t *testing.T) {
	declared := []Entry{{Name: "admin"}}
	current := []Entry{{Name: "admin"}, {Name: "sibling"}}

	add, remove := Plan(declared, nil, current)
	if len(add) != 0 {
		t.Errorf("nothing to add, got %v", add)
	}
	if len(remove) != 0 {
		t.Errorf("owning nothing must remove nothing, got %v", remove)
	}
}

func TestPlanAddsDeclaredMissingFromCurrent(t *testing.T) {
	add, remove := Plan(
		[]Entry{{Name: "admin"}, {Name: "auditor"}},
		nil,
		[]Entry{{Name: "admin"}},
	)
	if !reflect.DeepEqual(add, []Entry{{Name: "auditor"}}) {
		t.Errorf("add = %v, want the missing declared entry", add)
	}
	if len(remove) != 0 {
		t.Errorf("remove = %v, want nothing", remove)
	}
}

func TestPruneDropsEntriesNoLongerPresent(t *testing.T) {
	owned := []Entry{{Name: "admin"}, {Name: "gone"}}
	current := []Entry{{Name: "admin"}, {Name: "sibling"}}

	got := Prune(owned, current)
	if !reflect.DeepEqual(got, []Entry{{Name: "admin"}}) {
		t.Errorf("Prune() = %v, want only the entry still present", got)
	}
}

func TestPruneDoesNotAdoptUndeclaredEntries(t *testing.T) {
	// A declared entry that has not been added yet must not be adopted by
	// Prune, or a failed add would be recorded as applied and later removed.
	got := Prune([]Entry{{Name: "admin"}}, []Entry{{Name: "other"}})
	if len(got) != 0 {
		t.Errorf("Prune() = %v, want nothing adopted", got)
	}
}

func TestAdoptExcludesFailedAdditions(t *testing.T) {
	declared := []Entry{{Name: "admin"}}
	// current reflects Keycloak before the failed write.
	current := []Entry{{Name: "sibling"}}

	if got := Adopt(declared, current); len(got) != 0 {
		t.Errorf("Adopt() = %v, want a failed addition excluded", got)
	}
}

func TestReleaseReturnsOwnedStillPresent(t *testing.T) {
	owned := []Entry{{Name: "admin"}, {Name: "gone"}}
	current := []Entry{{Name: "admin"}, {Name: "sibling"}}

	got := Release(owned, current)
	if !reflect.DeepEqual(got, []Entry{{Name: "admin"}}) {
		t.Errorf("Release() = %v, want only the owned entry still present", got)
	}
}

func TestConvertProjectsFields(t *testing.T) {
	type spec struct{ id, name string }
	in := []spec{{id: "1", name: "admin"}, {name: "backups"}}

	got := Convert(in, func(s spec) Entry { return Entry{ID: s.id, Name: s.name} })
	want := []Entry{{ID: "1", Name: "admin"}, {Name: "backups"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Convert() = %v, want %v", got, want)
	}
}
