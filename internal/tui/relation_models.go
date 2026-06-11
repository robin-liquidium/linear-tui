package tui

// issueRelationTypeLabels lists Linear relation labels supported by the picker.
var issueRelationTypeLabels = []charmPickerItem{
	{ID: "blocking", Label: "blocking"},
	{ID: "blocked by", Label: "blocked by"},
	{ID: "related", Label: "related"},
	{ID: "duplicate", Label: "duplicate"},
	{ID: "similar", Label: "similar"},
}
