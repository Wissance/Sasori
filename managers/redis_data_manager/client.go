package redis_data_manager

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wissance/Ferrum/data"
	"github.com/wissance/Ferrum/managers/errors_managers"
	sf "github.com/wissance/stringFormatter"
)

func (mn *RedisDataManager) GetClientsFromRealm(realmName string) ([]data.Client, error) {
	realmClients, err := mn.getRealmClients(realmName)
	if err != nil {
		return nil, fmt.Errorf("getRealmClients failed: %w", err)
	}
	clients := make([]data.Client, len(realmClients))
	for i, rc := range realmClients {
		// todo(UMV) get all them at once
		client, err := mn.GetClient(realmName, rc.Name)
		if err != nil {
			if errors.Is(err, errors_managers.ErrNotFound) { // TODO(SIA) check
				mn.logger.Error(sf.Format("Realm: \"{0}\" has client: \"{1}\", that Redis does not have", realmName, rc.Name))
			}
			return nil, fmt.Errorf("GetClient failed: %w", err)
		}
		clients[i] = *client
	}
	return clients, nil
}

func (mn *RedisDataManager) GetClient(realmName string, clientName string) (*data.Client, error) {
	clientKey := sf.Format(clientKeyTemplate, mn.namespace, realmName, clientName)
	client, err := getObjectFromRedis[data.Client](mn.redisClient, mn.ctx, mn.logger, Client, clientKey)
	if err != nil {
		if errors.Is(err, errors_managers.ErrNotFound) {
			mn.logger.Error(sf.Format("Realm: \"{0}\" does not have Client: \"{1}\"", realmName, clientName))
		}
		return nil, fmt.Errorf("getObjectFromRedis failed: %w", err)
	}
	return client, nil
}

// Returns an error if the client exists in redis
func (mn *RedisDataManager) CreateClient(realmName string, clientNew data.Client) error {
	// TODO(SIA) транзакции
	// TODO(SIA) возможно нужно проверять, что есть какие-то поля у clients
	_, err := mn.GetClient(realmName, clientNew.Name) // TODO(SIA) use function isExists
	if err == nil {
		return errors_managers.ErrExists
	}
	if !errors.Is(err, errors_managers.ErrNotFound) {
		return fmt.Errorf("GetClient failed: %w", err)
	}
	_, err = mn.GetRealm(realmName) // TODO(SIA) use function isExists
	if err != nil {
		return fmt.Errorf("GetRealm failed: %w", err)
	}

	clientBytes, err := json.Marshal(clientNew)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during Client marshal")) // TODO(SIA) ADD NAME
		return fmt.Errorf("json.Marshal failed: %w", err)
	}
	err = mn.createClientRedis(realmName, clientNew.Name, string(clientBytes))
	if err != nil {
		return fmt.Errorf("createClientRedis failed: %w", err)
	}

	if err := mn.addClientToRealm(realmName, clientNew); err != nil {
		return fmt.Errorf("addClientToRealm failed: %w", err)
	}

	return nil
}

func (mn *RedisDataManager) DeleteClient(realmName string, clientName string) error {
	if err := mn.deleteClientRedis(realmName, clientName); err != nil {
		return fmt.Errorf("deleteClientRedis failed: %w", err)
	}
	if err := mn.deleteClientFromRealm(realmName, clientName); err != nil {
		if errors.Is(err, errors_managers.ErrNotFound) || errors.Is(err, errors_managers.ErrZeroLength) {
			return nil
		}
		return fmt.Errorf("deleteClientFromRealm failed: %w", err)
	}
	return nil
}

func (mn *RedisDataManager) UpdateClient(realmName string, clientName string, clientNew data.Client) error {
	// TODO(SIA) транзакции
	oldClient, err := mn.GetClient(realmName, clientName)
	if err != nil {
		return fmt.Errorf("GetClient failed: %w", err)
	}
	if clientNew.ID != oldClient.ID || clientNew.Name != oldClient.Name {
		if err := mn.DeleteClient(realmName, oldClient.Name); err != nil {
			return fmt.Errorf("DeleteClient failed: %w", err)
		}
		if err := mn.addClientToRealm(realmName, clientNew); err != nil {
			return fmt.Errorf("addClientToRealm failed: %w", err)
		}
	}

	clientBytes, err := json.Marshal(clientNew)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during Client marshal")) // TODO(SIA) ADD NAME
		return fmt.Errorf("json.Marshal failed: %w", err)
	}
	err = mn.createClientRedis(realmName, clientNew.Name, string(clientBytes))
	if err != nil {
		return fmt.Errorf("createClientRedis failed: %w", err)
	}

	return nil
}

func (mn *RedisDataManager) getRealmClients(realmName string) ([]data.ExtendedIdentifier, error) {
	realmClientsKey := sf.Format(realmClientsKeyTemplate, mn.namespace, realmName)
	realmClients, err := getObjectsListFromRedis[data.ExtendedIdentifier](mn.redisClient, mn.ctx, mn.logger, RealmClients, realmClientsKey)
	if err != nil {
		return nil, fmt.Errorf("getObjectsListFromRedis failed: %w", err)
	}
	if len(realmClients) == 0 {
		mn.logger.Error(sf.Format("There are no clients for realm: \"{0}\" in Redis", realmName))
		return nil, errors_managers.ErrZeroLength
	}
	return realmClients, nil
}

func (mn *RedisDataManager) getRealmClient(realmName string, clientName string) (*data.ExtendedIdentifier, error) {
	realmClients, err := mn.getRealmClients(realmName)
	if err != nil {
		return nil, fmt.Errorf("getRealmClients failed: %w", err)
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
		return nil, errors_managers.ErrNotFound
	}
	return &client, nil
}

// If such a key exists, the value will be overwritten without error
func (mn *RedisDataManager) createClientRedis(realmName string, clientName string, clientJson string) error {
	clientKey := sf.Format(clientKeyTemplate, mn.namespace, realmName, clientName)
	if err := setString(mn.redisClient, mn.ctx, mn.logger, Client, clientKey, clientJson); err != nil {
		// TODO(SIA) add log
		return fmt.Errorf("setString failed: %w", err)
	}
	return nil
}

func (mn *RedisDataManager) addClientToRealm(realmName string, client data.Client) error {
	realmClient := data.ExtendedIdentifier{
		ID:   client.ID,
		Name: client.Name,
	}
	sliceRealmClient := []data.ExtendedIdentifier{realmClient}
	if err := mn.createRealmClients(realmName, sliceRealmClient, false); err != nil {
		return fmt.Errorf("createRealmClients failed: %w", err)
	}
	return nil
}

// Adds clients to the realm. If the argument isAllPreDelete = true, all other clients will be deleted before they are added
func (mn *RedisDataManager) createRealmClients(realmName string, realmClients []data.ExtendedIdentifier, isAllPreDelete bool) error {
	bytesRealmClients, err := json.Marshal(realmClients)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during realmClients unmarshall"))
		return fmt.Errorf("json.Marshal failed: %w", err)
	}
	if isAllPreDelete {
		if err := mn.deleteRealmClientsRedis(realmName); err != nil {
			if err != nil && !errors.Is(err, errors_managers.ErrNotExists) {
				return fmt.Errorf("deleteRealmClientsRedis failed: %w", err)
			}
		}
	}
	realmClientsKey := sf.Format(realmClientsKeyTemplate, mn.namespace, realmName)
	redisIntCmd := mn.redisClient.RPush(mn.ctx, realmClientsKey, string(bytesRealmClients)) // TODO(SIA) переписать по аналогии с другими
	if redisIntCmd.Err() != nil {
		// TODO(SIA) add log
		return redisIntCmd.Err()
	}
	return nil
}

func (mn *RedisDataManager) deleteClientRedis(realmName string, clientName string) error {
	clientKey := sf.Format(clientKeyTemplate, mn.namespace, realmName, clientName)
	if err := delKey(mn.redisClient, mn.ctx, mn.logger, Client, clientKey); err != nil {
		return fmt.Errorf("delKey failed: %w", err)
	}
	return nil
}

// Deletes client from realmClients, does not delete client. Will return an error if there is no client in realm
func (mn *RedisDataManager) deleteClientFromRealm(realmName string, clientName string) error {
	// TODO(SIA) Много действий происходит, для удаления клиента: происходит получение клиентов, нахождение клиента, удаление его из массива,
	// удаление всех клиентов из редис, добавление нового массива клиентов в редис
	realmClients, err := mn.getRealmClients(realmName)
	if err != nil {
		return fmt.Errorf("getRealmClients failed: %w", err)
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
		// TODO(SIA) add log ("realm \"%s\" doesn't have client \"%s\" in Redis", realmName, clientName)
		return errors_managers.ErrNotFound
	}
	if err := mn.createRealmClients(realmName, realmClients, true); err != nil {
		return fmt.Errorf("createRealmClients failed: %w", err)
	}
	return nil
}
