package writeintent

// Phrase is a substring that served create/update descriptions must contain
// (or must not contain, for ForbiddenCreatePhrases).
type Phrase struct {
	Text   string
	Reason string
}

// CreateDescriptionPhrases is the steerability contract for a pair's create tool.
func CreateDescriptionPhrases(p Pair) []Phrase {
	return []Phrase{
		{Text: "net-new", Reason: "must name net-new write intent"},
		{Text: "Create once", Reason: "must say create once"},
		{Text: p.UpdateTool, Reason: "must name the refine tool"},
		{Text: "do not call " + p.CreateTool + " again", Reason: "must forbid same-turn re-create"},
		{Text: p.Resource + ".id", Reason: "must keep the returned id"},
	}
}

// UpdateDescriptionPhrases is the steerability contract for a pair's update tool.
func UpdateDescriptionPhrases(p Pair) []Phrase {
	return []Phrase{
		{Text: "Prefer this tool", Reason: "refine is the default after create"},
		{Text: "after create", Reason: "must sequence after create"},
		{Text: "existing " + p.Resource + " id", Reason: "must pass the known id"},
		{Text: "do not call " + p.CreateTool, Reason: "must not create-for-refine"},
		{Text: "full replacement", Reason: "must contrast with a future patch tool"},
	}
}

// ForbiddenCreatePhrases must not appear on create descriptions.
// Listing first is not required for a net-new create (ENG-1735).
func ForbiddenCreatePhrases() []Phrase {
	return []Phrase{
		{Text: "list_dashboards first", Reason: "must not require list-before-create"},
		{Text: "call list_dashboards before", Reason: "must not require list-before-create"},
		{Text: "list existing dashboards first", Reason: "must not require list-before-create"},
	}
}
