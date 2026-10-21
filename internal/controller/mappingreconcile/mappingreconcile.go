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

// Package mappingreconcile implements the additive reconciliation shared by the
// controllers that manage a membership set belonging to a parent object - the
// role and scope mappings under a client.
//
// A set is additive when a resource owns only the entries it declares. Several
// resources may then share one parent, each responsible for its own entries, and
// neither can disturb the other's.
//
// Ownership cannot be recomputed from the declared set on each pass. If it were,
// an entry dropped from the spec would vanish from the owned set too, and its
// removal would be unobservable - the resource would never take it back out of
// the parent. So ownership is the set a resource last applied, carried on
// status between passes, and removals are computed as owned-minus-declared.
package mappingreconcile

// Entry identifies one member of a parent's set. A spec may identify it by ID,
// by name, or by either, so an entry matches when either supplied field matches.
type Entry struct {
	// ID is the member's ID, when the spec identified it by ID.
	ID string

	// Name is the member's name, when the spec identified it by name.
	Name string
}

// Matches reports whether e and o identify the same member.
//
// Either field the spec supplied is enough to match, which lets an entry
// identified only by name be recognised before Keycloak has assigned the member
// an ID. This is the rule the mapping controllers used before the set became
// additive, kept unchanged: only ownership, not identification, is different.
func (e Entry) Matches(o Entry) bool {
	return (e.ID != "" && e.ID == o.ID) || (e.Name != "" && e.Name == o.Name)
}

// Convert projects a declared or owned set into Entries.
func Convert[S ~[]E, E any](in S, f func(E) Entry) []Entry {
	out := make([]Entry, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}

// UpToDate reports whether every declared entry is present in the parent's
// current set.
//
// The parent's set is not required to match the declared set exactly. Entries
// this resource does not own - those belonging to a sibling resource, or added
// out of band - are none of its business, so their presence does not make the
// resource out of date.
func UpToDate(declared, current []Entry) bool {
	for _, d := range declared {
		if !contains(current, d) {
			return false
		}
	}
	return true
}

// Plan computes the additions and removals needed to bring the parent's set in
// line with what this resource owns.
//
// Additions are declared entries the parent is missing. Removals are owned
// entries no longer declared - and only those. An owned entry is dropped from
// the owned set the moment it leaves the declared set, so without this the
// removal would never be issued.
//
// Entries in the parent's set that this resource never owned are never removed.
func Plan(declared, owned, current []Entry) (add, remove []Entry) {
	for _, d := range declared {
		if !contains(current, d) {
			add = append(add, d)
		}
	}
	for _, o := range owned {
		if !contains(declared, o) {
			remove = append(remove, o)
		}
	}
	return add, remove
}

// Prune drops owned entries that are no longer in the parent's set.
//
// Someone may remove an entry in Keycloak directly, or the parent may be
// recreated without them. Ownership has to follow reality, or the resource would
// go on owning entries that do not exist and try to remove them forever.
//
// Entries newly declared but not yet added are deliberately not adopted here.
// They are adopted by the pass that adds them, so ownership only ever grows
// through a successful write to Keycloak.
func Prune(owned, current []Entry) []Entry {
	out := make([]Entry, 0, len(owned))
	for _, o := range owned {
		if contains(current, o) {
			out = append(out, o)
		}
	}
	return out
}

// Adopt returns the owned set to record after a successful write: the declared
// entries, pruned against what the parent actually has, so a failed addition is
// never recorded as applied.
func Adopt(declared, current []Entry) []Entry {
	return Prune(declared, current)
}

// Release returns the entries a resource must remove from its parent when it is
// being deleted: the ones it owns that are still present.
//
// Removing the parent's whole set instead would strip entries belonging to
// sibling resources, which is the failure this package exists to prevent.
func Release(owned, current []Entry) []Entry {
	return Prune(owned, current)
}

func contains(set []Entry, e Entry) bool {
	for _, s := range set {
		if e.Matches(s) {
			return true
		}
	}
	return false
}
