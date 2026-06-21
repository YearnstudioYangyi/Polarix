package ActionPermissionType

type AllowedPermission int

const (
	SomeUser AllowedPermission = iota
	Admin
	AllUser
)
