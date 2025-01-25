package redis

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/wissance/Ferrum/config"
	"github.com/wissance/Ferrum/data"
	errors2 "github.com/wissance/Ferrum/errors"
	sf "github.com/wissance/stringFormatter"
)

// GetUsers function for getting all realm users
/* This function select all realm users (used by getRealmUsers) by constructing redis key from namespace and realm name
 * Probably in future this function could consume a lot of memory (if we would have a lot of users in a realm) probably we should limit amount of Users to fetch
 * This function works in two steps:
 *     1. Get all data.ExtendedIdentifier pairs id-name
 *     2. Get all User objects at once by key slices (every redis key for user combines from namespace, realm, username)
 * Parameters:
 *    - realmName - name of the realm
 * Returns slice of Users and error
 */
func (mn *RedisDataManager) GetUsers(realmName string) ([]data.User, error) {
	if !mn.IsAvailable() {
		return []data.User{}, errors2.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	// TODO(UMV): possibly we should not use this method ??? what if we have 1M+ users .... ? think maybe it should be somehow optimized ...
	realmUsers, err := mn.getRealmUsers(realmName)
	if err != nil {
		if errors.Is(err, errors2.ErrZeroLength) {
			return []data.User{}, nil
		}
		return nil, errors2.NewUnknownError("getRealmUsers", "RedisDataManager.GetUsers", err)
	}

	// todo(UMV): probably we should organize batching here if we have many users i.e. 100K+
	userRedisKeys := make([]string, len(realmUsers))
	for i, ru := range realmUsers {
		userRedisKeys[i] = sf.Format(userKeyTemplate, mn.namespace, realmName, ru.Name)
	}

	// userFullDataRealmsKey := sf.Format(realmUsersFullDataKeyTemplate, mn.namespace, realmName)
	// this is wrong, we can't get rawUsers such way ...
	realmUsersData, err := getMultipleRedisObjects[interface{}](mn.redisClient, mn.ctx, mn.logger, RealmUsers, userRedisKeys)
	if err != nil {
		return nil, errors2.NewUnknownError("getMultipleRedisObjects", "RedisDataManager.GetUsers", err)
	}
	// getObjectsListFromRedis[interface{}](mn.redisClient, mn.ctx, mn.logger, RealmUsers, userFullDataRealmsKey)
	if len(realmUsersData) == 0 {
		mn.logger.Error(sf.Format("Redis does not have all users that belong to Realm: \"{0}\"", realmName))
		return nil, err
	}
	if len(realmUsers) != len(realmUsersData) {
		mn.logger.Error(sf.Format("Realm: \"{0}\" has users, that Redis does not have part of it", realmName))
		return nil, errors2.ErrNotAll
	}

	userData := make([]data.User, len(realmUsersData))
	for i, u := range realmUsersData {
		userData[i] = data.CreateUser(u, nil)
	}
	return userData, nil
}

// GetUser function for getting realm user by username
/* This function constructs Redis key by pattern combines namespace, realm name and username (userKeyTemplate)
 * Parameters:
 *    - realmName
 *    - userName
 * Returns: User and error
 */
func (mn *RedisDataManager) GetUser(realmName string, userName string) (data.User, error) {
	if !mn.IsAvailable() {
		// todo(UMV): is this Valid or NOT ????
		return data.User(nil), errors2.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	userKey := sf.Format(userKeyTemplate, mn.namespace, realmName, userName)
	rawUser, err := getSingleRedisObject[interface{}](mn.redisClient, mn.ctx, mn.logger, User, userKey)
	if err != nil {
		if errors.As(err, &errors2.EmptyNotFoundErr) {
			return nil, err
		}
		return nil, errors2.NewUnknownError("getSingleRedisObject", "RedisDataManager.GetUser", err)
	}
	user := data.CreateUser(*rawUser, nil)
	return user, nil
}

// GetUserById function for getting realm user by userId
/* This function is more complex than GetUser, because we are using combination of realm name and username to store user data,
 * therefore this function extracts all realm users data and find appropriate by relation id-name after that it behaves like GetUser function
 * Parameters:
 *    - realmName
 *    - userId - identifier of searching user
 * Returns: User and error
 */
func (mn *RedisDataManager) GetUserById(realmName string, userId uuid.UUID) (data.User, error) {
	if !mn.IsAvailable() {
		return data.User(nil), errors2.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	realmUser, err := mn.getRealmUserById(realmName, userId)
	if err != nil {
		if errors.As(err, &errors2.EmptyNotFoundErr) {
			return nil, err
		}
		return nil, errors2.NewUnknownError("getRealmUserById", "RedisDataManager.GetUserById", err)
	}
	user, err := mn.GetUser(realmName, realmUser.Name)
	if err != nil {
		if errors.As(err, &errors2.EmptyNotFoundErr) {
			mn.logger.Error(sf.Format("Realm: \"{0}\" has user: \"{1}\", that Redis does not have", realmName, userId))
		}
		return nil, err
	}
	return user, nil
}

// CreateUser - new user creation
/* Returns an error if the user exists in redis
 * Arguments:
 *    - realmName
 *    - userNew
 * Returns: error
 */
func (mn *RedisDataManager) CreateUser(realmName string, userNew data.User) error {
	if !mn.IsAvailable() {
		return errors2.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	// TODO(SIA) Add transaction
	// TODO(SIA) use function isExists
	_, err := mn.GetRealm(realmName)
	if err != nil {
		mn.logger.Warn(sf.Format("CreateUser: GetRealmObject failed, error: {0}", err.Error()))
		return err
	}
	userName := userNew.GetUsername()
	// TODO(SIA) use function isExists
	_, err = mn.GetUser(realmName, userName)
	if err == nil {
		return errors2.NewObjectExistsError(string(User), userName, sf.Format("realm: {0}", realmName))
	}
	if !errors.As(err, &errors2.EmptyNotFoundErr) {
		mn.logger.Warn(sf.Format("CreateUser: GetUser failed, error: {0}", err.Error()))
		return err
	}
	upsertUserErr := mn.upsertUserObject(realmName, userName, userNew.GetJsonString())
	if upsertUserErr != nil {
		mn.logger.Error(sf.Format("CreateUser: addUserToRealm failed, error: {0}", upsertUserErr.Error()))
		return upsertUserErr
	}

	if addUserRealmErr := mn.addUserToRealm(realmName, userNew); addUserRealmErr != nil {
		mn.logger.Error(sf.Format("CreateUser: addUserToRealm failed, error: {0}", addUserRealmErr.Error()))
		return addUserRealmErr
	}

	return nil
}

// DeleteUser - deleting an existing user
/* It also deletes the user from realmUsers
 * Arguments:
 *    - realmName
 *    - userName
 * Returns: error
 */
func (mn *RedisDataManager) DeleteUser(realmName string, userName string) error {
	if !mn.IsAvailable() {
		return errors2.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	if err := mn.deleteUserObject(realmName, userName); err != nil {
		if errors.As(err, &errors2.EmptyNotFoundErr) {
			return err
		}
		return errors2.NewUnknownError("deleteUserObject", "RedisDataManager.DeleteUser", err)
	}
	if err := mn.deleteUserFromRealm(realmName, userName); err != nil {
		// todo(UMV): second errors.Is because ErrZeroLength doesn't have custom type
		if errors.As(err, &errors2.EmptyNotFoundErr) || errors.Is(err, errors2.ErrZeroLength) {
			return nil
		}
		return errors2.NewUnknownError("deleteUserFromRealm", "RedisDataManager.DeleteUser", err)
	}
	return nil
}

// UpdateUser - upgrading an existing user
/*
 * Arguments:
 *    - realmName
 *    - userName
 *    - userNew
 * Returns: error
 */
func (mn *RedisDataManager) UpdateUser(realmName string, userName string, userNew data.User) error {
	if !mn.IsAvailable() {
		return errors2.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	// TODO(SIA) Add transaction
	oldUser, err := mn.GetUser(realmName, userName)
	if err != nil {
		if errors.As(err, &errors2.EmptyNotFoundErr) {
			return err
		}
		return errors2.NewUnknownError("GetUser", "RedisDataManager.UpdateUser", err)
	}
	oldUserName := oldUser.GetUsername()
	oldUserId := oldUser.GetId()

	newUserName := userNew.GetUsername()
	newUserId := userNew.GetId()

	if newUserId != oldUserId || newUserName != oldUserName {
		if delUserErr := mn.DeleteUser(realmName, oldUserName); delUserErr != nil {
			return errors2.NewUnknownError("DeleteUser", "RedisDataManager.UpdateUser", delUserErr)
		}
		if addUserRealmErr := mn.addUserToRealm(realmName, userNew); addUserRealmErr != nil {
			return errors2.NewUnknownError("addUserToRealm", "RedisDataManager.UpdateUser", addUserRealmErr)
		}
	}

	err = mn.upsertUserObject(realmName, newUserName, userNew.GetJsonString())
	if err != nil {
		return errors2.NewUnknownError("upsertUserObject", "RedisDataManager.UpdateUser", err)
	}

	return nil
}

// SetPassword - setting a password for user
/*
 * Arguments:
 *    - realmName
 *    - userName
 *    - password - string
 * Returns: error
 */
func (mn *RedisDataManager) SetPassword(realmName string, userName string, password string) error {
	user, err := mn.GetUser(realmName, userName)
	if err != nil {
		return errors2.NewUnknownError("GetUser", "RedisDataManager.SetPassword", err)
	}
	realm, err := mn.GetRealm(realmName)
	if err != nil {
		return errors2.NewUnknownError("GetRealm", "RedisDataManager.SetPassword", err)
	}
	if setPasswordErr := user.SetPassword(password, realm.Encoder); setPasswordErr != nil {
		return errors2.NewUnknownError("SetPassword", "RedisDataManager.SetPassword", setPasswordErr)
	}
	if upsertUserErr := mn.upsertUserObject(realmName, userName, user.GetJsonString()); upsertUserErr != nil {
		return errors2.NewUnknownError("upsertUserObject", "RedisDataManager.SetPassword", upsertUserErr)
	}
	return nil
}

// getRealmUsers - get realmUsers entity.
/* realmUsersKeyTemplate is used inside.
 * Arguments:
 *    - realmName
 * Returns: slice of ExtendedIdentifier, error
 */
func (mn *RedisDataManager) getRealmUsers(realmName string) ([]data.ExtendedIdentifier, error) {
	userRealmsKey := sf.Format(realmUsersKeyTemplate, mn.namespace, realmName)
	realmUsers, err := getObjectsListOfSlicesItemsFromRedis[data.ExtendedIdentifier](mn.redisClient, mn.ctx, mn.logger, RealmUsers, userRealmsKey)
	if err != nil {
		if errors.Is(err, errors2.ErrZeroLength) {
			return nil, err
		}

		return nil, errors2.NewUnknownError("getObjectsListOfSlicesItemsFromRedis", "RedisDataManager.getRealmUsers", err)
	}
	return realmUsers, nil
}

// getRealmUser - get ExtendedIdentifier entity.
/* First, getRealmUsers happens. Then there are comparisons by name
 * Arguments:
 *    - realmName
 *    - userName
 * Returns: *ExtendedIdentifier, error
 */
// nolint unused
func (mn *RedisDataManager) getRealmUser(realmName string, userName string) (*data.ExtendedIdentifier, error) {
	realmUsers, err := mn.getRealmUsers(realmName)
	if err != nil {
		return nil, errors2.NewUnknownError("getRealmUsers", "RedisDataManager.getRealmUser", err)
	}

	var user data.ExtendedIdentifier
	userFound := false
	for _, rc := range realmUsers {
		if rc.Name == userName {
			userFound = true
			user = rc
			break
		}
	}
	if !userFound {
		mn.logger.Debug(sf.Format("User with name: \"{0}\" was not found for realm: \"{1}\"", userName, realmName))
		return nil, errors2.NewObjectNotFoundError(string(User), userName, sf.Format("realm: {0}", realmName))
	}
	return &user, nil
}

// getRealmUserById - get ExtendedIdentifier entity.
/* First, getRealmUsers happens. Then there are comparisons by id
 * Arguments:
 *    - realmName
 *    - userId
 * Returns: *ExtendedIdentifier, error
 */
func (mn *RedisDataManager) getRealmUserById(realmName string, userId uuid.UUID) (*data.ExtendedIdentifier, error) {
	realmUsers, err := mn.getRealmUsers(realmName)
	if err != nil {
		if errors.As(err, &errors2.EmptyNotFoundErr) {
			return nil, err
		}
		if errors.Is(err, errors2.ErrZeroLength) {
			return nil, errors2.NewObjectNotFoundError(string(User), userId.String(), sf.Format("realm: {0}", realmName))
		}
		return nil, errors2.NewUnknownError("getRealmUsers", "RedisDataManager.getRealmUserById", err)
	}
	var user data.ExtendedIdentifier
	userFound := false
	for _, rc := range realmUsers {
		if rc.ID == userId {
			userFound = true
			user = rc
			break
		}
	}
	if !userFound {
		mn.logger.Debug(sf.Format("User with id: \"{0}\" was not found for realm: \"{1}\"", userId, realmName))
		return nil, errors2.NewObjectNotFoundError(string(User), userId.String(), sf.Format("realm: {0}", realmName))
	}
	return &user, nil
}

// upsertUserObject - create or update a user
/* If such a key exists, the value will be overwritten without error
 * Arguments:
 *    - realmName
 *    - userName
 *    - userJson - string
 * Returns: error
 */
func (mn *RedisDataManager) upsertUserObject(realmName string, userName string, userJson string) error {
	userKey := sf.Format(userKeyTemplate, mn.namespace, realmName, userName)
	if err := mn.upsertRedisString(User, userKey, userJson); err != nil {
		return errors2.NewUnknownError("upsertRedisString", "RedisDataManager.upsertUserObject", err)
	}
	return nil
}

// addUserToRealm - adding a client to the realmUser entity
/* Uses createRealmUsers internally
 * Arguments:
 *    - realmName
 *    - user
 * Returns: error
 */
func (mn *RedisDataManager) addUserToRealm(realmName string, user data.User) error {
	userId := user.GetId()
	userName := user.GetUsername()
	realmUser := data.ExtendedIdentifier{
		ID:   userId,
		Name: userName,
	}
	sliceRealmUser := []data.ExtendedIdentifier{realmUser}
	if err := mn.createRealmUsers(realmName, sliceRealmUser, false); err != nil {
		return errors2.NewUnknownError("createRealmUsers", "RedisDataManager.addUserToRealm", err)
	}
	return nil
}

// createRealmUsers - To add a new item to the list or create a new realmUsers
/* Adds users to the realm. If the argument isAllPreDelete = true, all other users will be deleted before they are added
 * Arguments:
 *    - realmName
 *    - realmUsers - slice of ExtendedIdentifier
 *    - isAllPreDelete - flag, If true, the already existing realmUsers will be deleted. If false, new ones will be added to it.
 * Returns: error
 */
func (mn *RedisDataManager) createRealmUsers(realmName string, realmUsers []data.ExtendedIdentifier, isAllPreDelete bool) error {
	// TODO(SIA) maybe split into two functions
	bytesRealmUsers, err := json.Marshal(realmUsers)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during Marshal realmUsers"))
		return errors2.NewUnknownError("json.Marshal", "RedisDataManager.createRealmUsers", err)
	}

	if isAllPreDelete {
		if deleteRealmUserErr := mn.deleteRealmUsersObject(realmName); deleteRealmUserErr != nil {
			// todo(UMV): errors.Is because ErrNotExists doesn't have custom type
			if !errors.Is(deleteRealmUserErr, errors2.ErrNotExists) {
				return errors2.NewUnknownError("deleteRealmUsersObject", "RedisDataManager.createRealmUsers", deleteRealmUserErr)
			}
		}
	}
	realmUsersKey := sf.Format(realmUsersKeyTemplate, mn.namespace, realmName)
	if appendStringErr := mn.appendStringToRedisList(RealmUsers, realmUsersKey, string(bytesRealmUsers)); appendStringErr != nil {
		return errors2.NewUnknownError("appendStringToRedisList", "RedisDataManager.createRealmUsers", appendStringErr)
	}
	return nil
}

// deleteUserObject - deleting a user
/* Inside uses userKeyTemplate
 * Arguments:
 *    - realmName
 *    - userName
 * Returns: error
 */
func (mn *RedisDataManager) deleteUserObject(realmName string, userName string) error {
	userKey := sf.Format(userKeyTemplate, mn.namespace, realmName, userName)
	if err := mn.deleteRedisObject(User, userKey); err != nil {
		if errors.As(err, &errors2.EmptyNotFoundErr) {
			return err
		}
		return errors2.NewUnknownError("deleteRedisObject", "RedisDataManager.deleteUserObject", err)
	}
	return nil
}

// deleteUserFromRealm - deleting a user from realmUsers entity
/* Deletes user from realmUsers, does not delete user. Will return an error if there is no user in realm.
 * After deletion, all items in the list are merged into one.
 * A lot of things happen to delete a user: get users, find the user, delete it from the array,
 * delete all users from the realm, add a new array of users to the realm.
 * Arguments:
 *    - realmName
 *    - userName
 * Returns: error
 */
func (mn *RedisDataManager) deleteUserFromRealm(realmName string, userName string) error {
	realmUsers, err := mn.getRealmUsers(realmName)
	if err != nil {
		return errors2.NewUnknownError("getRealmUsers", "RedisDataManager.deleteUserFromRealm", err)
	}

	isHasUser := false
	for i := range realmUsers {
		if realmUsers[i].Name == userName {
			isHasUser = true
			if i != (len(realmUsers) - 1) {
				realmUsers[i] = realmUsers[len(realmUsers)-1]
			}
			realmUsers = realmUsers[:len(realmUsers)-1]
			break
		}
	}
	if !isHasUser {
		return errors2.NewObjectNotFoundError(string(User), userName, sf.Format("realm: {0}", realmName))
	}
	if createRealmUserErr := mn.createRealmUsers(realmName, realmUsers, true); createRealmUserErr != nil {
		return errors2.NewUnknownError("createRealmUsers", "RedisDataManager.deleteUserFromRealm", createRealmUserErr)
	}
	return nil
}
