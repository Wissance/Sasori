package data

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/ohler55/ojg/jp"
	"github.com/wissance/Ferrum/utils/encoding"
)

const (
	pathToPassword = "credentials.password"
)

// KeyCloakUser this structure is for user data that looks similar to KeyCloak, Users in Keycloak have info field with preferred_username and sub
// and others fields, Ferrum users have credentials built-in in user (temporary it stores in non encrypted mode)
type KeyCloakUser struct {
	rawData     interface{}
	jsonRawData string
}

// CreateUser function creates User interface instance (KeyCloakUser) from raw json
/* Function create User instance from any json (interface{})
 * Parameters:
 *    - rawData - any json
 * Return: instance of User as KeyCloakUser
 */
func CreateUser(rawData interface{}, encoder *encoding.PasswordJsonEncoder) User {
	jsonData, _ := json.Marshal(&rawData)
	kcUser := &KeyCloakUser{rawData: rawData, jsonRawData: string(jsonData)}
	password := getPathStringValue[string](kcUser.rawData, pathToPassword)
	if encoder != nil {
		// todo(UMV): handle CreateUser errors in the future
		_ = kcUser.SetPassword(password, encoder)
	}
	user := User(kcUser)
	return user
}

// GetUsername returns username as it stores in KeyCloak
/* this function use internal map to navigate over info.preferred_username keys, the last one key is a login key
 * We are expecting that username is unique in the Realm
 * Parameters: no
 * Returns: username
 */
func (user *KeyCloakUser) GetUsername() string {
	return getPathStringValue[string](user.rawData, "info.preferred_username")
}

// GetPasswordHash returns hash of password
/* this function use internal map to navigate over credentials.password keys to retrieve a hash of password
 * Parameters: no
 * Returns: hash of password
 */
// todo(UMV): we should consider case when User is External
func (user *KeyCloakUser) GetPasswordHash() string {
	password := getPathStringValue[string](user.rawData, pathToPassword)
	return password
}

// SetPassword
/* this function changes a raw password to its hash in the user's rawData and jsonRawData and sets it
 * Parameters:
 *	- password - new password
 *	- encoder - encoder object with salt and hasher
 */
func (user *KeyCloakUser) SetPassword(password string, encoder *encoding.PasswordJsonEncoder) error {
	hashed := encoder.GetB64PasswordHash(password)
	if err := setPathStringValue(user.rawData, pathToPassword, hashed); err != nil {
		return err
	}
	jsonData, _ := json.Marshal(&user.rawData)
	user.jsonRawData = string(jsonData)
	return nil
}

// GetId returns unique user identifier
/* this function use internal map to navigate over info.sun keys to retrieve a user id
 * Parameters: no
 * Returns: user id
 */
func (user *KeyCloakUser) GetId() uuid.UUID {
	idStrValue := getPathStringValue[string](user.rawData, "info.sub")
	id, err := uuid.Parse(idStrValue)
	// nolint staticcheck
	if err != nil {
		// todo(UMV): think what to do here, return error!
	}
	return id
}

// GetUserInfo returns Json with all non-confidential user data as KeyCloak do
/* this function use internal map to navigate over key info ant retrieve all public userinfo
 * Parameters: no
 * Returns: user info
 */
func (user *KeyCloakUser) GetUserInfo() interface{} {
	result := getPathStringValue[interface{}](user.rawData, "info")
	return result
}

func (user *KeyCloakUser) GetRawData() interface{} {
	return user.rawData
}

func (user *KeyCloakUser) GetJsonString() string {
	return user.jsonRawData
}

// IsFederatedUser returns bool if user storing externally, if user is external, password can't be stored in storage
/* this function determines whether user stores outside the database i.e. in ActiveDirectory or other systems
 * navigation property for this federation.name
 * Parameters: no
 */
func (user *KeyCloakUser) IsFederatedUser() bool {
	result := getPathStringValue[string](user.rawData, "federation.name")
	return len(result) > 0
}

func (user *KeyCloakUser) GetFederationId() string {
	result := getPathStringValue[string](user.rawData, "federation.name")
	return result
}

// getPathStringValue is a generic function to get actually map by key, key represents as a jsonpath navigation property
/* this function uses json path to navigate over nested maps and return any required type
 * Parameters:
 *    - rawData - json object
 *    - path - json path to retrieve part of json with specified type (T)
 * Returns: part of json
 */
func getPathStringValue[T any](rawData interface{}, path string) T {
	var result T
	mask, err := jp.ParseString(path)
	// nolint staticcheck
	if err != nil {
		// todo(UMV): log and think what to do ...
	}
	res := mask.Get(rawData)
	if len(res) == 1 {
		result = res[0].(T)
	}
	return result
}

// setPathStringValue is a function to search data by path and set data by key, key represents as a jsonpath navigation property
/* this function uses json path to navigate over nested maps and set data
 * Parameters:
 *    - rawData - json object
 *    - path - json path to retrieve part of json
 *    - value - value to be set to rawData
 */
func setPathStringValue(rawData interface{}, path string, value string) error {
	mask, err := jp.ParseString(path)
	if err != nil {
		return fmt.Errorf("jp.ParseString failed: %w", err)
	}
	if err := mask.Set(rawData, value); err != nil {
		return fmt.Errorf("jp.Set failed: %w", err)
	}
	return nil
}
