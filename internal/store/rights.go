package store

const (
	RightRead        = "read"
	RightEdit        = "edit"
	RightReview      = "review"
	RightUpload      = "upload"
	RightManageUsers = "manage_users"
)

var allRights = []string{RightRead, RightEdit, RightReview, RightUpload, RightManageUsers}

func AllRights() []string {
	return append([]string(nil), allRights...)
}
