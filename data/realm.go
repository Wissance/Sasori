package data

import "github.com/wissance/Ferrum/utils/encoding"

// Realm is a struct that describes typical Realm
/* It was originally designed to efficiently work in memory with small amount of data therefore it contains relations with Clients and Users
 * But in a systems with thousands of users working at the same time it is too expensive to fetch Realm with all relations therefore
 * in such systems Clients && Users would be empty, and we should to get User or Client separately
 */
type Realm struct {
	Name                   string                        `json:"name"`
	Clients                []Client                      `json:"clients"`
	Users                  []interface{}                 `json:"users"`
	TokenExpiration        int                           `json:"token_expiration"`
	RefreshTokenExpiration int                           `json:"refresh_expiration"`
	UserFederationServices []UserFederationServiceConfig `json:"user_federation_services"`
	PasswordSalt           string                        `json:"password_salt"`
	Encoder                encoding.PasswordJsonEncoder
}
