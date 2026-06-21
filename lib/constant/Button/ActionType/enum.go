package ActionType

type ButtonAction int

const (
	Link ButtonAction = iota
	Callback
	Command
)

func IsVaildActionType(actionType ButtonAction) bool {
	return int(Link) >= 0 && int(actionType) <= int(Command)
}
