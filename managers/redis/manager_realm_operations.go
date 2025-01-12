package redis

import (
	"encoding/json"
	"errors"

	"github.com/wissance/Ferrum/config"
	"github.com/wissance/Ferrum/data"
	appErrs "github.com/wissance/Ferrum/errors"
	"github.com/wissance/Ferrum/utils/encoding"
	sf "github.com/wissance/stringFormatter"
)

// GetRealm function for getting realm by name, returns the realm with clients but no users.
/* This function constructs Redis key by pattern combines namespace and realm name (realmKeyTemplate). Unlike from FILE provider.
 * Realm stored in Redis does not have Clients and Users inside Realm itself, these objects must be queried separately.
 * Parameters:
 *     - realmName name of a realm
 * Returns: Tuple - realm and error
 */
func (mn *RedisDataManager) GetRealm(realmName string) (*data.Realm, error) {
	if !mn.IsAvailable() {
		return nil, appErrs.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}

	realm, err := mn.getRealmObject(realmName)
	if err != nil {
		if errors.As(err, &appErrs.EmptyNotFoundErr) {
			return nil, err
		}
		return nil, appErrs.NewUnknownError("getRealmObject", "RedisDataManager.GetRealm", err)
	}

	// should get realms too
	// if realms were stored without clients (we expected so), get clients related to realm and assign here
	clients, err := mn.GetClients(realmName)
	if err != nil {
		if !errors.Is(err, appErrs.ErrZeroLength) {
			return nil, appErrs.NewUnknownError("GetClients", "RedisDataManager.GetRealm", err)
		}
	}
	realm.Clients = clients

	configs, err := mn.GetUserFederationConfigs(realmName)
	if err != nil {
		if !errors.Is(err, appErrs.ErrZeroLength) {
			return nil, appErrs.NewUnknownError("GetUserFederationConfigs", "RedisDataManager.GetRealm", err)
		}
	}
	realm.UserFederationServices = configs
	realm.Encoder = encoding.NewPasswordJsonEncoder(realm.PasswordSalt)

	return realm, nil
}

// CreateRealm - creates a realm, if the realm has users and clients, they will also be created.
/* Create Realm, if it contains User s it creates them too:
 * 1. Check realm by name, realmName MUST be unique
 * 2. Iterate over Client's, Create Clients
 * 3. Create Client's - Realm connection
 * 4. Iterate over User's, Create Users
 * 5. Create User's - Realm connection
 * 6. Create Realm
 * Arguments:
 *    - newRealm - newly creating realm body data with Clients and Users
 * Returns: error
 */
func (mn *RedisDataManager) CreateRealm(newRealm data.Realm) error {
	if !mn.IsAvailable() {
		return appErrs.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	// TODO(SIA) Add transaction
	// TODO(SIA) use function isExists
	_, err := mn.GetRealm(newRealm.Name)
	if err == nil {
		return appErrs.NewObjectExistsError(string(Realm), newRealm.Name, "")
	}
	if !errors.As(err, &appErrs.EmptyNotFoundErr) {
		return err
	}

	if len(newRealm.Clients) != 0 {
		realmClients := make([]data.ExtendedIdentifier, len(newRealm.Clients))
		for i, client := range newRealm.Clients {
			bytesClient, marshallErr := json.Marshal(client)
			if marshallErr != nil {
				mn.logger.Error(sf.Format("An error occurred during Marshal Client: {0}", marshallErr.Error()))
				return appErrs.NewUnknownError("json.Marshal", "RedisDataManager.CreateRealm", marshallErr)
			}
			if upsertClientErr := mn.upsertClientObject(newRealm.Name, client.Name, string(bytesClient)); upsertClientErr != nil {
				return appErrs.NewUnknownError("upsertClientObject", "RedisDataManager.CreateRealm", upsertClientErr)
			}
			realmClients[i] = data.ExtendedIdentifier{
				ID:   client.ID,
				Name: client.Name,
			}
		}
		if createRealmClientErr := mn.createRealmClients(newRealm.Name, realmClients, true); createRealmClientErr != nil {
			return appErrs.NewUnknownError("createRealmClients", "RedisDataManager.CreateRealm", createRealmClientErr)
		}
	}

	salt := encoding.GenerateRandomSalt()

	if len(newRealm.Users) != 0 {
		realmUsers := make([]data.ExtendedIdentifier, len(newRealm.Users))
		encoder := encoding.NewPasswordJsonEncoder(salt)
		for i, user := range newRealm.Users {
			newUser := data.CreateUser(user, encoder)
			newUserName := newUser.GetUsername()
			if upsertUserErr := mn.upsertUserObject(newRealm.Name, newUserName, newUser.GetJsonString()); upsertUserErr != nil {
				return appErrs.NewUnknownError("upsertUserObject", "RedisDataManager.CreateRealm", upsertUserErr)
			}
			newUserId := newUser.GetId()
			realmUsers[i] = data.ExtendedIdentifier{
				ID:   newUserId,
				Name: newUserName,
			}
		}
		if createUserRealmErr := mn.createRealmUsers(newRealm.Name, realmUsers, true); createUserRealmErr != nil {
			return appErrs.NewUnknownError("createRealmUsers", "RedisDataManager.CreateRealm", createUserRealmErr)
		}
	}

	shortRealm := data.Realm{
		Name:                   newRealm.Name,
		Clients:                []data.Client{},
		Users:                  []any{},
		TokenExpiration:        newRealm.TokenExpiration,
		RefreshTokenExpiration: newRealm.RefreshTokenExpiration,
		PasswordSalt:           salt,
		Encoder:                encoding.PasswordJsonEncoder{},
	}
	jsonShortRealm, err := json.Marshal(shortRealm)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during Marshal Realm: {0}", err.Error()))
		return appErrs.NewUnknownError("json.Marshal", "RedisDataManager.CreateRealm", err)
	}
	if upsertRealmErr := mn.upsertRealmObject(newRealm.Name, string(jsonShortRealm)); upsertRealmErr != nil {
		return appErrs.NewUnknownError("upsertRealmObject", "RedisDataManager.CreateRealm", upsertRealmErr)
	}

	// Creating UserFederationServiceConfig[] after Realm creation
	if len(newRealm.UserFederationServices) > 0 {
		for _, userFederationCfg := range newRealm.UserFederationServices {
			createUserFederationServiceErr := mn.CreateUserFederationConfig(newRealm.Name, userFederationCfg)
			if createUserFederationServiceErr != nil {
				return appErrs.NewUnknownError("createUserFederationService", "RedisDataManager.CreateRealm",
					createUserFederationServiceErr)
			}
		}
	}

	return nil
}

// DeleteRealm - deleting the realm with all it Client's and User's
/* 1. Get Client's associated with a realm
 * 2. Iterate over Client's, Delete Client's
 * 3. Delete relation Realm - Client's
 * 4. Get Realm User
 * 5. Iterate over User's, Delete User's
 * 6. Delete Realm
 * Arguments:
 *    - realmName - name of a Realm to Delete
 * Returns: error
 */
func (mn *RedisDataManager) DeleteRealm(realmName string) error {
	if !mn.IsAvailable() {
		return appErrs.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	// TODO(SIA) Add transaction
	if err := mn.deleteRealmObject(realmName); err != nil {
		if errors.As(err, &appErrs.EmptyNotFoundErr) {
			return err
		}
		return appErrs.NewUnknownError("deleteRealmObject", "RedisDataManager.DeleteRealm", err)
	}

	clients, err := mn.getRealmClients(realmName)
	if err != nil {
		// todo(UMV): errors.Is because ErrZeroLength doesn't have custom type
		if !errors.Is(err, appErrs.ErrZeroLength) {
			return appErrs.NewUnknownError("getRealmClients", "RedisDataManager.DeleteRealm", err)
		}
	} else {
		// TODO(SIA) overwrite to delete all keys at once
		for _, client := range clients {
			if deleteClientErr := mn.deleteClientObject(realmName, client.Name); deleteClientErr != nil {
				return appErrs.NewUnknownError("deleteClientObject", "RedisDataManager.DeleteRealm", deleteClientErr)
			}
		}
		if deleteRealmClientErr := mn.deleteRealmClientsObject(realmName); deleteRealmClientErr != nil {
			return appErrs.NewUnknownError("deleteRealmClientsObject", "RedisDataManager.DeleteRealm", deleteRealmClientErr)
		}
	}

	users, err := mn.getRealmUsers(realmName)
	if err != nil {
		// todo(UMV): second errors.Is because ErrZeroLength doesn't have custom type
		if !errors.Is(err, appErrs.ErrZeroLength) {
			return appErrs.NewUnknownError("getRealmUsers", "RedisDataManager.DeleteRealm", err)
		}
	} else {
		// TODO(SIA) overwrite to delete all keys at once
		for _, user := range users {
			if deleteUserErr := mn.deleteUserObject(realmName, user.Name); deleteUserErr != nil {
				return appErrs.NewUnknownError("deleteUserObject", "RedisDataManager.DeleteRealm", deleteUserErr)
			}
		}
		if deleteRealmUserErr := mn.deleteRealmUsersObject(realmName); deleteRealmUserErr != nil {
			return appErrs.NewUnknownError("deleteRealmUsersObject", "RedisDataManager.DeleteRealm", deleteRealmUserErr)
		}
	}

	userFederationConfigs, err := mn.GetUserFederationConfigs(realmName)
	if err != nil {
		if !errors.Is(err, appErrs.ErrZeroLength) {
			return appErrs.NewUnknownError("GetUserFederationConfigs", "RedisDataManager.DeleteRealm", err)
		}
	} else {
		for _, userFederation := range userFederationConfigs {
			if deleteUserFederationErr := mn.DeleteUserFederationConfig(realmName, userFederation.Name); deleteUserFederationErr != nil {
				return appErrs.NewUnknownError("deleteUserFederationConfigObject", "RedisDataManager.DeleteRealm", deleteUserFederationErr)
			}
		}
	}

	return nil
}

// UpdateRealm - realm update. It is expected that realmValue will not contain clients and users.
/* If the name or id of the realm has changed.  Then this information will be cascaded to all dependent objects.
 * Arguments:
 *    - realmName
 *    - realmNew
 * Returns: error
 */
func (mn *RedisDataManager) UpdateRealm(realmName string, realmNew data.Realm) error {
	if !mn.IsAvailable() {
		return appErrs.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	// TODO(SIA) Add transaction
	oldRealm, err := mn.getRealmObject(realmName)
	if err != nil {
		return err
	}
	if oldRealm.Name != realmNew.Name {
		// TODO(SIA) use function isExists
		_, getRealmErr := mn.getRealmObject(realmNew.Name)
		if getRealmErr == nil {
			mn.logger.Error(sf.Format("Realm with a new name \"{0}\" already exists in Redis", realmNew.Name))
			return appErrs.ErrExists
		}
		if !errors.As(getRealmErr, &appErrs.ObjectNotFoundError{}) {
			return appErrs.NewUnknownError("getRealmObject", "RedisDataManager.UpdateRealm", getRealmErr)
		}

		clients, getClientsErr := mn.GetClients(oldRealm.Name)
		// todo(UMV): errors.Is because ErrZeroLength doesn't have custom type
		if getClientsErr != nil && !errors.Is(getClientsErr, appErrs.ErrZeroLength) {
			return appErrs.NewUnknownError("GetClients", "RedisDataManager.UpdateRealm", getClientsErr)
		}
		users, getUsersErr := mn.GetUsers(oldRealm.Name)
		// todo(UMV): errors.Is because ErrZeroLength doesn't have custom type
		if getUsersErr != nil && !errors.Is(getUsersErr, appErrs.ErrZeroLength) {
			return appErrs.NewUnknownError("GetUsers", "RedisDataManager.UpdateRealm", getUsersErr)
		}
		usersData := make([]any, len(users))
		for i, u := range users {
			usersData[i] = u.GetRawData()
		}
		newRealmWithOldClientsAndUsers := data.Realm{
			Name:                   realmNew.Name,
			Clients:                clients,
			Users:                  usersData,
			TokenExpiration:        realmNew.TokenExpiration,
			RefreshTokenExpiration: realmNew.RefreshTokenExpiration,
		}
		if deleteRealmErr := mn.DeleteRealm(oldRealm.Name); deleteRealmErr != nil {
			return appErrs.NewUnknownError("DeleteRealm", "RedisDataManager.UpdateRealm", deleteRealmErr)
		}
		if createRealmErr := mn.CreateRealm(newRealmWithOldClientsAndUsers); createRealmErr != nil {
			return appErrs.NewUnknownError("CreateRealm", "RedisDataManager.UpdateRealm", createRealmErr)
		}
		return nil
	}

	shortRealm := data.Realm{
		Name:                   realmNew.Name,
		Clients:                []data.Client{},
		Users:                  []any{},
		TokenExpiration:        realmNew.TokenExpiration,
		RefreshTokenExpiration: realmNew.RefreshTokenExpiration,
	}
	jsonShortRealm, err := json.Marshal(shortRealm)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during Marshal Realm: {0}", err.Error()))
		return appErrs.NewUnknownError("json.Marshal", "RedisDataManager.UpdateRealm", err)
	}
	if upsertRealmErr := mn.upsertRealmObject(shortRealm.Name, string(jsonShortRealm)); upsertRealmErr != nil {
		return appErrs.NewUnknownError("upsertRealmObject", "RedisDataManager.UpdateRealm", upsertRealmErr)
	}
	return nil
}

// getRealmObject - getting realm without clients and users
/*
 * Arguments:
 *    - realmName
 * Returns: *Realm, error
 */
func (mn *RedisDataManager) getRealmObject(realmName string) (*data.Realm, error) {
	realmKey := sf.Format(realmKeyTemplate, mn.namespace, realmName)
	realm, err := getSingleRedisObject[data.Realm](mn.redisClient, mn.ctx, mn.logger, Realm, realmKey)
	if err != nil {
		if errors.As(err, &appErrs.EmptyNotFoundErr) {
			mn.logger.Debug(sf.Format("Redis does not have Realm: \"{0}\"", realmName))
		}
		return nil, err
	}
	return realm, nil
}

// upsertRealmObject - create or update a realm without clients and users
/* If such a key exists, the value will be overwritten without error
 * Arguments:
 *    - realmName
 *    - realmJson - string
 * Returns: *Realm, error
 */
func (mn *RedisDataManager) upsertRealmObject(realmName string, realmJson string) error {
	realmKey := sf.Format(realmKeyTemplate, mn.namespace, realmName)
	if err := mn.upsertRedisString(Realm, realmKey, realmJson); err != nil {
		return appErrs.NewUnknownError("upsertRedisString", "RedisDataManager.upsertRealmObject", err)
	}
	return nil
}

// deleteRealmObject - deleting a realm without clients and users
/* Inside uses realmKeyTemplate
 * Arguments:
 *    - realmName
 * Returns: error
 */
func (mn *RedisDataManager) deleteRealmObject(realmName string) error {
	realmKey := sf.Format(realmKeyTemplate, mn.namespace, realmName)
	if err := mn.deleteRedisObject(Realm, realmKey); err != nil {
		if errors.As(err, &appErrs.EmptyNotFoundErr) {
			return err
		}
		return appErrs.NewUnknownError("deleteRedisObject", "RedisDataManager.deleteRealmObject", err)
	}
	return nil
}

// deleteRealmClientsObject - deleting realmClients only
/* Inside uses realmClientsKeyTemplate
 * Arguments:
 *    - realmName
 * Returns: error
 */
func (mn *RedisDataManager) deleteRealmClientsObject(realmName string) error {
	realmClientsKey := sf.Format(realmClientsKeyTemplate, mn.namespace, realmName)
	if err := mn.deleteRedisObject(RealmClients, realmClientsKey); err != nil {
		if !errors.As(err, &appErrs.EmptyNotFoundErr) {
			return appErrs.NewUnknownError("deleteRedisObject", "RedisDataManager.deleteRealmClientsObject", err)
		}
	}
	return nil
}

// deleteRealmUsersObject - deleting realmUsers only
/* Inside uses realmUsersKeyTemplate
 * Arguments:
 *    - realmName
 * Returns: error
 */
func (mn *RedisDataManager) deleteRealmUsersObject(realmName string) error {
	realmUsersKey := sf.Format(realmUsersKeyTemplate, mn.namespace, realmName)
	if err := mn.deleteRedisObject(RealmUsers, realmUsersKey); err != nil {
		if !errors.As(err, &appErrs.EmptyNotFoundErr) {
			return appErrs.NewUnknownError("deleteRedisObject", "RedisDataManager.deleteRealmUsersObject", err)
		}
	}
	return nil
}
