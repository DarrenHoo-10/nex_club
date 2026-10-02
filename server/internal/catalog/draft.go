package catalog

// HasUnpublishedDraft is the only definition of an unpublished draft.
// A pointer equal to the current published revision is not a draft.
// Publish still clears the pointer.
func HasUnpublishedDraft(draftID, publishedRevisionID *RevisionID) bool {
	if draftID == nil {
		return false
	}
	if publishedRevisionID == nil {
		return true
	}
	return *draftID != *publishedRevisionID
}
