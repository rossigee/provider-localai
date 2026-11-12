package clients

import "testing"

func TestRecordFirstObservationAdoptsUnknownObject(t *testing.T) {
	o := ""
	RecordFirstObservation(&o, "")
	if o != OwnershipAdopted {
		t.Errorf("ownership = %q, want adopted for an object seen for the first time", o)
	}
}

func TestRecordFirstObservationKeepsKnownOwnership(t *testing.T) {
	cases := []struct {
		name, ownership, statusName, want string
	}{
		{"created stays created", OwnershipCreated, "", OwnershipCreated},
		{"legacy resource with a name stays unmarked", "", "bookkeeping", ""},
		{"already adopted stays adopted", OwnershipAdopted, "bookkeeping", OwnershipAdopted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := c.ownership
			RecordFirstObservation(&o, c.statusName)
			if o != c.want {
				t.Errorf("ownership = %q, want %q", o, c.want)
			}
		})
	}
}

func TestDeletesExternal(t *testing.T) {
	if DeletesExternal(OwnershipAdopted) {
		t.Error("adopted objects must not be deleted with the resource")
	}
	for _, o := range []string{OwnershipCreated, ""} {
		if !DeletesExternal(o) {
			t.Errorf("ownership %q should delete the LocalAI object", o)
		}
	}
}
