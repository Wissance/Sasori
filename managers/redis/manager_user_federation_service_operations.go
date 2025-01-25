package redis

import (
	"encoding/json"
	"errors"

	"github.com/wissance/Ferrum/config"
	"github.com/wissance/Ferrum/data"
	appErrs "github.com/wissance/Ferrum/errors"
	sf "github.com/wissance/stringFormatter"
)

// GetUserFederationConfig return data.UserFederationServiceConfig of configured Federation service
/* This function constructs Redis key by pattern combines namespace and realm name and config name (realmUserFederationService)
 * all Realm Federation Config stores in Redis List Object
 * Parameters:
 *     - realmName - name of a Realm
 *     - configName - name of a User Federation Service config
 * Returns: config and error
 */
func (mn *RedisDataManager) GetUserFederationConfig(realmName string, configName string) (*data.UserFederationServiceConfig, error) {
	if !mn.IsAvailable() {
		return nil, appErrs.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}

	realmUserFederationServiceConfigKey := sf.Format(realmUserFederationServiceTemplate, mn.namespace, realmName)
	realmUserFederationConfig, err := getObjectsListOfNonSlicesItemsFromRedis[data.UserFederationServiceConfig](mn.redisClient, mn.ctx, mn.logger, RealmUserFederationConfig,
		realmUserFederationServiceConfigKey)
	if err != nil {
		if errors.Is(err, appErrs.ErrZeroLength) {
			return nil, appErrs.NewObjectNotFoundError(realmUserFederationServiceTemplate, configName, sf.Format("realm: {0}", realmName))
		}
		return nil, err
	}
	for _, v := range realmUserFederationConfig {
		if v.Name == configName {
			return &v, err
		}
	}
	return nil, appErrs.NewObjectNotFoundError(realmUserFederationServiceTemplate, configName, sf.Format("realm: {0}", realmName))
}

func (mn *RedisDataManager) GetUserFederationConfigs(realmName string) ([]data.UserFederationServiceConfig, error) {
	if !mn.IsAvailable() {
		return []data.UserFederationServiceConfig{}, appErrs.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}

	realmUserFederationServiceConfigKey := sf.Format(realmUserFederationServiceTemplate, mn.namespace, realmName)
	realmUserFederationConfig, err := getObjectsListOfNonSlicesItemsFromRedis[data.UserFederationServiceConfig](mn.redisClient, mn.ctx, mn.logger,
		RealmUserFederationConfig, realmUserFederationServiceConfigKey)
	return realmUserFederationConfig, err
}

// CreateUserFederationConfig creates new data.UserFederationServiceConfig related to data.Realm by name
/* This function constructs Redis key by pattern combines namespace and realm name and config name (realmUserFederationService)
 * and creates config, unlike Users or Clients number of UserFederationConfig is not big, therefore we don't create a new sub-storage
 * Parameters:
 *     - realmName - name of a Realm
 *     - userFederationConfig - newly creating object data.UserFederationServiceConfig
 * Returns: error
 */
func (mn *RedisDataManager) CreateUserFederationConfig(realmName string, userFederationConfig data.UserFederationServiceConfig) error {
	if !mn.IsAvailable() {
		return appErrs.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	_, err := mn.getRealmObject(realmName)
	if err != nil {
		return err
	}
	// TODO(UMV): use function isExists
	cfg, err := mn.GetUserFederationConfig(realmName, userFederationConfig.Name)
	if cfg != nil {
		return appErrs.NewObjectExistsError(string(RealmUserFederationConfig), userFederationConfig.Name, sf.Format("realm: {0}", realmName))
	}
	if !errors.As(err, &appErrs.ObjectNotFoundError{}) {
		return err
	}

	userFederationConfigBytes, err := json.Marshal(userFederationConfig)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during Marshal UserFederationServiceConfig: {0}", err.Error()))
		return appErrs.NewUnknownError("json.Marshal", "RedisDataManager.CreateUserFederationConfig", err)
	}
	err = mn.createUserFederationConfigObject(realmName, string(userFederationConfigBytes))
	if err != nil {
		return appErrs.NewUnknownError("createUserFederationConfigObject", "RedisDataManager.CreateUserFederationConfig", err)
	}

	return nil
}

// UpdateUserFederationConfig - updating an existing data.UserFederationServiceConfig
/*  Just upsert object
 * Arguments:
 *    - realmName - name of a data.Realm
 *    - configName - name of a data.UserFederationServiceConfig
 *    - userFederationConfig - new User Federation Service Config body
 * Returns: error
 */
func (mn *RedisDataManager) UpdateUserFederationConfig(realmName string, configName string, userFederationConfig data.UserFederationServiceConfig) error {
	if !mn.IsAvailable() {
		return appErrs.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}
	_, err := mn.GetUserFederationConfig(realmName, configName)
	if err != nil {
		if errors.As(err, &appErrs.EmptyNotFoundErr) {
			return err
		}
		return appErrs.NewUnknownError("GetUserFederationConfig", "RedisDataManager.UpdateUserFederationConfig", err)
	}

	configBytes, err := json.Marshal(userFederationConfig)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during Marshal UserFederationServiceConfig: {0}", err.Error()))
		return appErrs.NewUnknownError("json.Marshal", "RedisDataManager.UpdateUserFederationConfig", err)
	}

	err = mn.updateUserFederationConfigObject(realmName, userFederationConfig.Name, string(configBytes))
	if err != nil {
		return appErrs.NewUnknownError("updateUserFederationConfigObject", "RedisDataManager.UpdateUserFederationConfig", err)
	}

	return nil
}

// DeleteUserFederationConfig removes data.UserFederationServiceConfig from storage
/* It simply removes data.UserFederationServiceConfig by key based on realmName + configName
 * Arguments:
 *    - realmName - name of a data.Realm
 *    - configName - name of a data.UserFederationServiceConfig
 * Returns: error
 */
func (mn *RedisDataManager) DeleteUserFederationConfig(realmName string, configName string) error {
	if !mn.IsAvailable() {
		return appErrs.NewDataProviderNotAvailable(string(config.REDIS), mn.redisOption.Addr)
	}

	cfg, err := mn.GetUserFederationConfig(realmName, configName)
	if err != nil {
		if errors.As(err, &appErrs.EmptyNotFoundErr) {
			return err
		}
		return appErrs.NewUnknownError("GetUserFederationConfig", "RedisDataManager.DeleteUserFederationConfig", err)
	}

	value, _ := json.Marshal(&cfg)

	if err = mn.deleteUserFederationConfigObject(realmName, string(value)); err != nil {
		if errors.As(err, &appErrs.EmptyNotFoundErr) {
			return err
		}
		return appErrs.NewUnknownError("deleteUserFederationConfigObject", "RedisDataManager.DeleteUserFederationConfig", err)
	}

	return nil
}

// createUserFederationConfigObject - create (append )data.UserFederationServiceConfig to appropriate LIST object related to a Realm
/* We don't check whether we have such item in LIST or not here because we do it in CreateUserFederationConfig function
 * Arguments:
 *    - realmName name of a data.Realm
 *    - userFederationJson - string with serialized (Marshalled object)
 * Returns: error
 */
func (mn *RedisDataManager) createUserFederationConfigObject(realmName string, userFederationJson string) error {
	realmConfigsKey := sf.Format(realmUserFederationServiceTemplate, mn.namespace, realmName)
	_, err := getObjectsListOfNonSlicesItemsFromRedis[data.UserFederationServiceConfig](mn.redisClient, mn.ctx, mn.logger, RealmUserFederationConfig, realmConfigsKey)
	if err != nil {
		if errors.Is(err, appErrs.ErrZeroLength) {
		} else {
			return appErrs.NewUnknownError("getObjectsListOfNonSlicesItemsFromRedis", "RedisDataManager.createUserFederationConfigObject", err)
		}
	}

	if err = mn.appendStringToRedisList(RealmUserFederationConfig, realmConfigsKey, userFederationJson); err != nil {
		return appErrs.NewUnknownError("upsertRedisString", "RedisDataManager.createUserFederationConfigObject", err)
	}
	return nil
}

// createUserFederationConfigObject - updates a data.UserFederationServiceConfig
/* We are iterating here through whole list of data.UserFederationServiceConfig related to Realm with realmName, if there are no such item,
 * an error of type appErrs.ObjectNotFoundError will be rise up
 * Arguments:
 *    - realmName name of a data.Realm
 *    - userFederation - user federation
 * Returns: error
 */
func (mn *RedisDataManager) updateUserFederationConfigObject(realmName string, userFederationName string, userFederationJson string /**data.UserFederationServiceConfig*/) error {
	realmConfigsKey := sf.Format(realmUserFederationServiceTemplate, mn.namespace, realmName)
	configs, err := getObjectsListOfNonSlicesItemsFromRedis[data.UserFederationServiceConfig](mn.redisClient, mn.ctx, mn.logger, RealmUserFederationConfig, realmConfigsKey)
	if err != nil {
		if errors.Is(err, appErrs.ErrZeroLength) {
		} else {
			return appErrs.NewUnknownError("getObjectsListOfNonSlicesItemsFromRedis", "RedisDataManager.updateUserFederationConfigObject", err)
		}
	}

	for k, v := range configs {
		if v.Name == userFederationName {
			return updateObjectListItemInRedis[string](mn.redisClient, mn.ctx, mn.logger, RealmUserFederationConfig,
				realmConfigsKey, int64(k), userFederationJson)
		}
	}

	return appErrs.NewObjectNotFoundError(string(RealmUserFederationConfig), userFederationName, sf.Format("Realm: {0}", realmName))
}

// deleteUserFederationConfigObject - deleting a data.UserFederationServiceConfig
/* Inside uses realmUserFederationService
 * Arguments:
 *    - realmName - name of data.Realm
 *    - configName - name of data.UserFederationServiceConfig
 * Returns: error
 */
func (mn *RedisDataManager) deleteUserFederationConfigObject(realmName string, value string) error {
	configKey := sf.Format(realmUserFederationServiceTemplate, mn.namespace, realmName)
	if err := mn.deleteRedisListItem(RealmUserFederationConfig, configKey, value); err != nil {
		if errors.As(err, &appErrs.EmptyNotFoundErr) {
			return err
		}
		return appErrs.NewUnknownError("deleteUserFederationConfigObject", "RedisDataManager.deleteUserFederationConfigObject", err)
	}
	return nil
}
