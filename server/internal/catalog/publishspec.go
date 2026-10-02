package catalog

// PublishSpec is the pure content check used before a projection is written.
// Tag resolution stays in the publication transaction because it reads the dictionary.
type PublishSpec struct{}

func (PublishSpec) Check(payload Payload) error {
	return ValidatePayload(payload)
}
