package data

import (
	"github.com/google/uuid"
	"github.com/wissance/Ferrum/utils/encoding"
)

// User is a common user interface with all Required methods to get information about user, in future we probably won't have GetPassword method
// because Password is not an only method for authentication
type User interface {
	GetUsername() string
	GetPasswordHash() string
	SetPassword(password string, encoder encoding.PasswordJsonEncoder) error
	GetId() uuid.UUID
	GetUserInfo() interface{}
	GetRawData() interface{}
	GetJsonString() string
	IsFederatedUser() bool
	// GetFederationId actually Federation Name
	GetFederationId() string
}

var _ User = (*KeyCloakUser)(nil)
