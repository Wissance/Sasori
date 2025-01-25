package operations

type ResourceType string

const (
	RealmResource                ResourceType = "realm"
	ClientResource               ResourceType = "client"
	UserResource                 ResourceType = "user"
	UserFederationConfigResource ResourceType = "user_federation"
)

type OperationType string

const (
	GetOperation    OperationType = "get"
	CreateOperation OperationType = "create"
	DeleteOperation OperationType = "delete"
	UpdateOperation OperationType = "update"
	ChangePassword  OperationType = "change_password"
	ResetPassword   OperationType = "reset_password"
)
