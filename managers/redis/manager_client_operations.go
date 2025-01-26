package redis

import (
	"encoding/json"
	"errors"

	"github.com/wissance/Ferrum/config"
	"github.com/wissance/Ferrum/data"
	errors2 "github.com/wissance/Ferrum/errors"
	sf "github.com/wissance/stringFormatter"
)

// GetClients - getting clients from the specified realm
/* 1. Get Realm clients short info by realmName
 * 2. Iterate over clients short info and build full Client data
 * Arguments:
 *    - realmName
 * Returns: Tuple = slice of client, error
 */
func (mn *RedisDataManager) GetClients(realmName string) ([]data.Client, error) {
	if !mn.IsAvailable() {
		return []data.Client{}, errors2.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}

	realmClients, err := mn.getRealmClients(realmName)
	if err != nil {
		// empty clients is not an error
		if !errors.Is(err, errors2.ErrZeroLength) {
			return nil, errors2.NewUnknownError("getRealmClients", "RedisDataManager.GetClients", err)
		}
	}
	clients := make([]data.Client, len(realmClients))
	for i, rc := range realmClients {
		// todo(UMV) get all them at once
		client, readClientErr := mn.GetClient(realmName, rc.Name)
		if readClientErr != nil {
			if errors.As(readClientErr, &errors2.ObjectNotFoundError{}) {
				mn.logger.Error(sf.Format("Realm: \"{0}\" has client: \"{1}\", that Redis does not have", realmName, rc.Name))
			}
			return nil, readClientErr
		}
		clients[i] = *client
	}
	return clients, nil
}

// GetClient function for get realm client by name
/* This function constructs Redis key by pattern combines namespace and realm name and client name (clientKeyTemplate)
 * Parameters:
 *     - realmName - name of a realm
 *     - clientName - name of a client
 * Returns: client and error
 */
func (mn *RedisDataManager) GetClient(realmName string, clientName string) (*data.Client, error) {
	if !mn.IsAvailable() {
		return nil, errors2.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}

	clientKey := sf.Format(clientKeyTemplate, mn.namespace, realmName, clientName)
	client, err := getSingleRedisObject[data.Client](mn.redisClient, mn.ctx, mn.logger, Client, clientKey)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// CreateClient - new client creation
/* Returns an error if the client exists in redis. Clients with same name could exist in different realms, but pair realmName, clientName
 * must be unique!
 * 1. Check Realm, that is not possible to create client in non-existing Realm
 * 2. Check Client, if we found we are rising error
 * Arguments:
 *    - realmName - name of a Realm that newly creating Client is associated
 *    - clientNew - new Client data (body)
 * Returns: error if creation failed, otherwise - nil
 */
func (mn *RedisDataManager) CreateClient(realmName string, clientNew data.Client) error {
	if !mn.IsAvailable() {
		return errors2.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	// TODO(SIA) Add transaction
	// TODO(SIA) use function isExists
	_, err := mn.getRealmObject(realmName)
	if err != nil {
		return err
	}
	// TODO(SIA) use function isExists
	_, err = mn.GetClient(realmName, clientNew.Name)
	if err == nil {
		return errors2.NewObjectExistsError(string(Client), clientNew.Name, sf.Format("realm: {0}", realmName))
	}
	if !errors.As(err, &errors2.ObjectNotFoundError{}) {
		return err
	}

	clientBytes, err := json.Marshal(clientNew)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during Marshal Client: {0}", err.Error()))
		return errors2.NewUnknownError("json.Marshal", "RedisDataManager.CreateClient", err)
	}
	err = mn.upsertClientObject(realmName, clientNew.Name, string(clientBytes))
	if err != nil {
		return errors2.NewUnknownError("upsertClientObject", "RedisDataManager.CreateClient", err)
	}

	if addClientErr := mn.addClientToRealm(realmName, &clientNew); addClientErr != nil {
		return errors2.NewUnknownError("addClientToRealm", "RedisDataManager.CreateClient", addClientErr)
	}

	return nil
}

// DeleteClient - deleting an existing client by pair (realmName, clientName)
/* It also deletes the client from realmClients, clients && realmClients stored in a separate collections
 * Arguments:
 *    - realmName - name of a realm
 *    - clientName - name of a client
 * Returns: error
 */
func (mn *RedisDataManager) DeleteClient(realmName string, clientName string) error {
	if !mn.IsAvailable() {
		return errors2.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}

	if err := mn.deleteClientObject(realmName, clientName); err != nil {
		if errors.As(err, &errors2.EmptyNotFoundErr) {
			return err
		}
		return errors2.NewUnknownError("deleteClientObject", "RedisDataManager.DeleteClient", err)
	}
	if err := mn.deleteClientFromRealm(realmName, clientName); err != nil {
		// todo(UMV): second errors.Is because ErrZeroLength doesn't have custom type
		if errors.As(err, &errors2.ObjectNotFoundError{}) || errors.Is(err, errors2.ErrZeroLength) {
			return nil
		}
		return err
	}
	return nil
}

// UpdateClient - updating an existing client
/* 1. Removes Client fully from clients and realm clients collections
 * 2. Creates client with new body (clientNew)
 * 3. Add relations between Realm and Client
 * Arguments:
 *    - realmName - name of a realm
 *    - clientName - name of a client
 *    - clientNew - new client body
 * Returns: error
 */
func (mn *RedisDataManager) UpdateClient(realmName string, clientName string, clientNew data.Client) error {
	if !mn.IsAvailable() {
		return errors2.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	// TODO(SIA) Add transaction
	oldClient, err := mn.GetClient(realmName, clientName)
	if err != nil {
		if errors.As(err, &errors2.EmptyNotFoundErr) {
			return err
		}
		return errors2.NewUnknownError("GetClient", "RedisDataManager.UpdateClient", err)
	}
	if clientNew.ID != oldClient.ID || clientNew.Name != oldClient.Name {
		if delErr := mn.DeleteClient(realmName, oldClient.Name); delErr != nil {
			return errors2.NewUnknownError("DeleteClient", "RedisDataManager.UpdateClient", delErr)
		}
		if addClientRealmErr := mn.addClientToRealm(realmName, &clientNew); addClientRealmErr != nil {
			return errors2.NewUnknownError("addClientToRealm", "RedisDataManager.UpdateClient", addClientRealmErr)
		}
	}

	clientBytes, err := json.Marshal(clientNew)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during Marshal Client: {0}", err.Error()))
		return errors2.NewUnknownError("json.Marshal", "RedisDataManager.UpdateClient", err)
	}

	err = mn.upsertClientObject(realmName, clientNew.Name, string(clientBytes))
	if err != nil {
		return errors2.NewUnknownError("upsertClientObject", "RedisDataManager.UpdateClient", err)
	}

	return nil
}

// getRealmClients - get realmClients entity.
/* realmClientsKeyTemplate is used inside.
 * Arguments:
 *    - realmName
 * Returns: Tuple: slice of ExtendedIdentifier, error
 */
func (mn *RedisDataManager) getRealmClients(realmName string) ([]data.ExtendedIdentifier, error) {
	realmClientsKey := sf.Format(realmClientsKeyTemplate, mn.namespace, realmName)
	realmClients, err := getObjectsListOfSlicesItemsFromRedis[data.ExtendedIdentifier](mn.redisClient, mn.ctx, mn.logger, RealmClients, realmClientsKey)
	if err != nil {
		if errors.Is(err, errors2.ErrZeroLength) {
			return nil, err
		}
		return nil, errors2.NewUnknownError("getObjectsListOfSlicesItemsFromRedis", "RedisDataManager.getRealmClients", err)
	}
	return realmClients, nil
}

// getRealmClient - get ExtendedIdentifier entity.
/* First, getRealmClients happens. Then there are comparisons by name
 * Arguments:
 *    - realmName
 *    - clientName
 * Returns: *ExtendedIdentifier, error
 */
// nolint unused
func (mn *RedisDataManager) getRealmClient(realmName string, clientName string) (*data.ExtendedIdentifier, error) {
	realmClients, err := mn.getRealmClients(realmName)
	if err != nil {
		return nil, errors2.NewUnknownError("getRealmClients", "RedisDataManager.getRealmClient", err)
	}

	realmHasClient := false
	var client data.ExtendedIdentifier
	for _, rc := range realmClients {
		if rc.Name == clientName {
			realmHasClient = true
			client = rc
			break
		}
	}
	if !realmHasClient {
		mn.logger.Debug(sf.Format("Realm: \"{0}\" doesn't have Client: \"{1}\" in Redis", realmName, clientName))
		return nil, errors2.NewObjectNotFoundError(string(Realm), realmName, "")
	}
	return &client, nil
}

// upsertClientObject - create or update a client
/* If such a key exists, the value will be overwritten without error
 * Arguments:
 *    - realmName
 *    - clientName
 *    - clientJson - string
 * Returns: error
 */
func (mn *RedisDataManager) upsertClientObject(realmName string, clientName string, clientJson string) error {
	clientKey := sf.Format(clientKeyTemplate, mn.namespace, realmName, clientName)
	if err := mn.upsertRedisString(Client, clientKey, clientJson); err != nil {
		return errors2.NewUnknownError("upsertRedisString", "RedisDataManager.upsertClientObject", err)
	}
	return nil
}

// addClientToRealm - adding a client to the realmClient entity
/* Uses createRealmClients internally
 * Arguments:
 *    - realmName - name of a realm
 *    - client - client adding to realm
 * Returns: error
 */
func (mn *RedisDataManager) addClientToRealm(realmName string, client *data.Client) error {
	realmClient := data.ExtendedIdentifier{
		ID:   client.ID,
		Name: client.Name,
	}
	sliceRealmClient := []data.ExtendedIdentifier{realmClient}
	if err := mn.createRealmClients(realmName, sliceRealmClient, false); err != nil {
		return errors2.NewUnknownError("createRealmClients", "RedisDataManager.addClientToRealm", err)
	}
	return nil
}

// createRealmClients - To add a new item to the list or create a new realmClients
/* Adds clients to the realm. If the argument isAllPreDelete = true, all other clients will be deleted before they are added
 * Arguments:
 *    - realmName
 *    - realmClients - slice of ExtendedIdentifier
 *    - isAllPreDelete - flag, If true, the already existing realmClients will be deleted. If false, new ones will be added to it.
 * Returns: error
 */
func (mn *RedisDataManager) createRealmClients(realmName string, realmClients []data.ExtendedIdentifier, isAllPreDelete bool) error {
	// TODO(SIA) maybe split into two functions
	bytesRealmClients, err := json.Marshal(realmClients)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during Marshal realmClients: {0}", err.Error()))
		return errors2.NewUnknownError("json.Marshal", "RedisDataManager.createRealmClients", err)
	}
	if isAllPreDelete {
		if delErr := mn.deleteRealmClientsObject(realmName); delErr != nil {
			// todo(UMV): errors.Is because ErrZeroLength doesn't have custom type
			if !errors.Is(delErr, errors2.ErrNotExists) {
				return errors2.NewUnknownError("deleteRealmClientsObject", "RedisDataManager.createRealmClients", delErr)
			}
		}
	}
	realmClientsKey := sf.Format(realmClientsKeyTemplate, mn.namespace, realmName)
	if stringAppendErr := mn.appendStringToRedisList(RealmClients, realmClientsKey, string(bytesRealmClients)); stringAppendErr != nil {
		return errors2.NewUnknownError("appendStringToRedisList", "RedisDataManager.createRealmClients", stringAppendErr)
	}
	return nil
}

// deleteClientObject - deleting a client
/* Inside uses clientKeyTemplate
 * Arguments:
 *    - realmName
 *    - clientName
 * Returns: error
 */
func (mn *RedisDataManager) deleteClientObject(realmName string, clientName string) error {
	clientKey := sf.Format(clientKeyTemplate, mn.namespace, realmName, clientName)
	if err := mn.deleteRedisObject(Client, clientKey); err != nil {
		if errors.As(err, &errors2.EmptyNotFoundErr) {
			return err
		}
		return errors2.NewUnknownError("deleteRedisObject", "RedisDataManager.deleteClientObject", err)
	}
	return nil
}

// deleteClientFromRealm - deleting a client from realmClients entity
/* Deletes client from realmClients, does not delete client. Will return an error if there is no client in realm.
 * After deletion, all items in the list are merged into one.
 * A lot of things happen to delete a client: get clients, find the client, delete it from the array,
 * delete all clients from the realm, add a new array of clients to the realm.
 * Arguments:
 *    - realmName
 *    - clientName
 * Returns: error
 */
func (mn *RedisDataManager) deleteClientFromRealm(realmName string, clientName string) error {
	realmClients, err := mn.getRealmClients(realmName)
	if err != nil {
		mn.logger.Warn(sf.Format("deleteClientFromRealm failed: {0}", err.Error()))
		return err
	}

	isHasClient := false
	for i := range realmClients {
		if realmClients[i].Name == clientName {
			isHasClient = true
			if i != (len(realmClients) - 1) {
				realmClients[i] = realmClients[len(realmClients)-1]
			}
			realmClients = realmClients[:len(realmClients)-1]
			break
		}
	}
	if !isHasClient {
		return errors2.NewObjectNotFoundError(string(Client), clientName, sf.Format("realm: {0}", realmName))
	}
	if createClientErr := mn.createRealmClients(realmName, realmClients, true); createClientErr != nil {
		return errors2.NewUnknownError("createRealmClients", "RedisDataManager.deleteClientFromRealm", createClientErr)
	}
	return nil
}
