package clients

// Ownership values recorded in status.atProvider.ownership of Agent, Model and
// Skill resources.
const (
	// OwnershipCreated means this provider created the object in LocalAI.
	OwnershipCreated = "created"
	// OwnershipAdopted means the object was already in LocalAI when this
	// resource first observed it. Deleting the resource leaves it in place.
	OwnershipAdopted = "adopted"
)

// RecordFirstObservation marks an existing object as adopted when this
// resource has no record of it: no ownership yet and no name in status. Call it
// before status.name is set. A resource that already has a name, or was
// created here, keeps its ownership. Resources from before ownership was
// recorded have a name and no ownership, and so stay owned.
func RecordFirstObservation(ownership *string, statusName string) {
	if *ownership == "" && statusName == "" {
		*ownership = OwnershipAdopted
	}
}

// DeletesExternal reports whether deleting the resource should also delete the
// LocalAI object. Only adopted objects are kept.
func DeletesExternal(ownership string) bool {
	return ownership != OwnershipAdopted
}
