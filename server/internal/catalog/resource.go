package catalog

import (
	"encoding/json"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

// Resource is the aggregate that owns the draft pointer, field locks, and edit version.
type Resource struct {
	ID                ResourceID
	Kind              Kind
	Slug              Slug
	Status            Status
	Identity          *IdentityKey
	EditVersion       int64
	Locks             FieldLockSet
	FreshnessEligible bool
	IsDemo            bool
	DraftRevisionID   *RevisionID
	FirstPublishedAt  *time.Time
}

func NewDraft(id ResourceID, kind Kind, slug Slug, demo, fresh bool) *Resource {
	return &Resource{
		ID:                id,
		Kind:              kind,
		Slug:              slug,
		Status:            StatusDraft,
		EditVersion:       1,
		FreshnessEligible: fresh,
		IsDemo:            demo,
	}
}

// ApplyWrite merges locks for human and import saves, then applies unlock_fields.
// Pipeline saves do not add locks. The new draft id is attached and the version increments.
func (r *Resource) ApplyWrite(expected int64, origin Origin, changed []FieldPath, unlock []string, draft RevisionID) error {
	if err := ParseEditVersion(expected); err != nil {
		return err
	}
	if expected != r.EditVersion {
		return apperr.EditConflict("内容已被他人更新")
	}
	if origin.LocksFields() {
		r.Locks.Add(changed...)
	}
	if err := r.Locks.Unlock(r.Kind, unlock); err != nil {
		return err
	}
	r.DraftRevisionID = &draft
	r.EditVersion++
	return nil
}

// InitialLocks protects the first human or import snapshot without bumping the version.
func (r *Resource) InitialLocks(origin Origin, changed []FieldPath) {
	if origin.LocksFields() {
		r.Locks.Add(changed...)
	}
}

func (r *Resource) AttachDraft(id RevisionID) {
	r.DraftRevisionID = &id
}

// ApplyIdentity sets a confirmed key. A different existing key is rejected and not overwritten.
func (r *Resource) ApplyIdentity(next *IdentityKey) error {
	if next == nil {
		return nil
	}
	if r.Identity != nil && *r.Identity != *next {
		return apperr.Invalid("身份键与已有值不一致", apperr.FieldError{Field: "identity_key", Code: "conflict"})
	}
	copied := *next
	r.Identity = &copied
	return nil
}

// MarkPublished records the first publication time once, clears the draft, and bumps the version.
func (r *Resource) MarkPublished(at time.Time) {
	if r.FirstPublishedAt == nil {
		utc := at.UTC()
		r.FirstPublishedAt = &utc
	}
	r.Status = StatusPublished
	r.DraftRevisionID = nil
	r.EditVersion++
}

func (r *Resource) SetVisibility(next Status, expected int64) error {
	if err := ParseEditVersion(expected); err != nil {
		return err
	}
	if expected != r.EditVersion {
		return apperr.EditConflict("内容已被他人更新")
	}
	if !r.Status.CanTransit(next) {
		return apperr.Invalid("不能切换到该状态", apperr.FieldError{Field: "status", Code: "invalid"})
	}
	r.Status = next
	r.EditVersion++
	return nil
}

// KeepLocked copies protected fields from base onto proposed.
func KeepLocked(kind Kind, base, proposed Payload, locks FieldLockSet) (Payload, error) {
	if !locksHasAny(locks) {
		return proposed, nil
	}
	out := proposed
	if locks.Has("title") {
		out.Title = base.Title
	}
	if locks.Has("aliases") {
		out.Aliases = slicesClone(base.Aliases)
	}
	if locks.Has("summary") {
		out.Summary = base.Summary
	}
	if locks.Has("body_markdown") {
		out.BodyMarkdown = base.BodyMarkdown
	}
	if locks.Has("cover_urls") {
		out.CoverURLs = slicesClone(base.CoverURLs)
	}
	if locks.Has("primary_category_id") {
		out.PrimaryCategoryID = cloneTag(base.PrimaryCategoryID)
	}
	if locks.Has("tag_ids") {
		out.TagIDs = append([]TagID(nil), base.TagIDs...)
	}
	if locks.Has("quality_score") {
		out.QualityScore = base.QualityScore
	}
	if locks.Has("recommendation_reason") {
		out.Recommendation = base.Recommendation
	}
	merged, err := keepLockedDetails(kind, base.Details, proposed.Details, locks)
	if err != nil {
		return Payload{}, err
	}
	out.Details = merged
	return out, nil
}

func locksHasAny(locks FieldLockSet) bool {
	return len(locks.Slice()) > 0
}

func keepLockedDetails(kind Kind, base, proposed Details, locks FieldLockSet) (Details, error) {
	if base == nil || proposed == nil {
		return nil, apperr.Invalid("专属字段不正确", apperr.FieldError{Field: "details", Code: "invalid"})
	}
	locked := false
	for _, key := range DetailKeys(kind) {
		if locks.Has(FieldPath("details." + key)) {
			locked = true
			break
		}
	}
	if !locked {
		return proposed, nil
	}
	oldRaw, err := base.MarshalPublic()
	if err != nil {
		return nil, err
	}
	newRaw, err := proposed.MarshalPublic()
	if err != nil {
		return nil, err
	}
	var oldMap, newMap map[string]any
	if err := json.Unmarshal(oldRaw, &oldMap); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(newRaw, &newMap); err != nil {
		return nil, err
	}
	for _, key := range DetailKeys(kind) {
		if !locks.Has(FieldPath("details." + key)) {
			continue
		}
		if value, ok := oldMap[key]; ok {
			newMap[key] = value
		} else {
			delete(newMap, key)
		}
	}
	merged, err := json.Marshal(newMap)
	if err != nil {
		return nil, err
	}
	return ParseDetails(kind, merged)
}

func slicesClone(items []string) []string {
	if items == nil {
		return []string{}
	}
	return append([]string(nil), items...)
}

func cloneTag(id *TagID) *TagID {
	if id == nil {
		return nil
	}
	copied := *id
	return &copied
}
