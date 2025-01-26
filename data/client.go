package data

import (
	"github.com/google/uuid"
)

// ClientType is type of client security, Confidential clients must provide ClientSecret
type ClientType string

const (
	Public       ClientType = "public"
	Confidential ClientType = "confidential"
)

// Client is a realm client, represents an application nad set of rules for interacting with Authorization server
type Client struct {
	Type ClientType
	ID   uuid.UUID
	Name string
	Auth Authentication
}
