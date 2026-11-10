package last9api

// CreatedVia records which tool and client created a resource. Embed it in request bodies
// that accept the fields; the API rejects any created_via other than the ones it knows.
type CreatedVia struct {
	CreatedVia       string `json:"created_via,omitempty"`
	CreatedViaClient string `json:"created_via_client,omitempty"`
}
