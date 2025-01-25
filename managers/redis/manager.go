package redis

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/wissance/Ferrum/errors"

	"github.com/redis/go-redis/v9"
	"github.com/wissance/Ferrum/config"
	"github.com/wissance/Ferrum/logging"
	sf "github.com/wissance/stringFormatter"
)

// This set of const of a templates to all data storing in Redis it contains prefix - a namespace {0}
const (
	userKeyTemplate                    = "{0}.{1}_user_{2}"
	realmKeyTemplate                   = "{0}.realm_{1}"
	realmClientsKeyTemplate            = "{0}.realm_{1}_clients"
	clientKeyTemplate                  = "{0}.{1}_client_{2}"
	realmUsersKeyTemplate              = "{0}.realm_{1}_users"
	realmUserFederationServiceTemplate = "{0}.realm_{1}_user_federations"
	// realmUsersFullDataKeyTemplate = "{0}.realm_{1}_users_full_data"
)

type objectType string

const (
	Realm                     objectType = "realm"
	RealmClients              objectType = "realm clients"
	RealmUsers                objectType = "realm users"
	RealmUserFederationConfig objectType = " realm user federation config"
	Client                    objectType = "client"
	User                      objectType = "user"
)

const defaultNamespace = "fe"

// RedisDataManager is a redis client
/*
 * Redis Data Manager is a service class for managing authorization server data in Redis
 * There are following store Rules:
 * 1. Realms (data.Realm) in Redis storing separately from Clients && Users, every Realm stores in Redis by key forming from template && Realm name
 *    i.e. if we have Realm with name "wissance" it could be accessed by key fe.realm_wissance (realmKeyTemplate)
 * 2. Realm Clients ([]data.ExtendedIdentifier) storing in Redis by key forming from template, Realm with name wissance has array of clients id by key
 *    fe.realm_wissance_clients (realmClientsKeyTemplate)
 * 3. Every Client (data.Client) stores separately by key forming from client name (different realms could have clients with same name but in different realm,
 *    Client Name is unique only in Realm) and template clientKeyTemplate, therefore realm with pair (ID: 6e09faca-1004-11ee-be56-0242ac120002 Name: homeApp)
 *    could be received by key - fe.wissance_client_homeApp
 * 4. Every User in Redis storing by it own key forming by userName + template (userKeyTemplate) -> i.e. user with (ID: 6e09faca-1004-11ee-be56-0242ac120002 Name: homeApp) stored
 *    by key fe.wissance_user_homeApp
 * 5. Client to Realm and User to Realm relation stored by separate keys forming using template and realm name, these relations stores array of data.ExtendedIdentifier
 *    that wires together Realm Name with User.ID and User.Name.
 *    IMPORTANT NOTES:
 *    1. When save Client or User don't forget to save relations in Redis too (see templates realmClientsKeyTemplate && realmUsersKeyTemplate)
 *    2. When add/modify new or existing user don't forget to update realmUsersFullDataKeyTemplate maybe this collection will be removed in future but currently
 *       we have it.
 */
type RedisDataManager struct {
	namespace   string
	redisOption *redis.Options
	redisClient *redis.Client
	logger      *logging.AppLogger
	ctx         context.Context
}

// IsAvailable methods that checks whether DataContext could be used or not
/* Availability means that redisClient is not NULL and Ready for receive requests
 * Parameters: no
 * Returns true if DataContext is available
 */
func (mn *RedisDataManager) IsAvailable() bool {
	if mn.redisClient == nil {
		mn.logger.Debug("Redis client was not initialized")
		return false
	}
	cmd := mn.redisClient.Ping(mn.ctx)
	_, err := cmd.Result()
	if err != nil {
		mn.logger.Debug(sf.Format("Redis Ping executed with error: {0}", err.Error()))
	}
	return err == nil
}

// CreateRedisDataManager is factory function for instance of RedisDataManager creation
/* Simply creates instance of RedisDataManager and initializes redis client, this function requires config.Namespace to be set up in configs, otherwise
 * defaultNamespace is using
 * Parameters:
 *     - dataSourceCfg contains Redis specific settings in Options map (see allowed keys of map in config.DataSourceConnOption)
 *     - logger - initialized logger instance
 */
func CreateRedisDataManager(dataSourceCfg *config.DataSourceConfig, logger *logging.AppLogger) (*RedisDataManager, error) {
	// todo(UMV): todo provide an error handling
	opts := buildRedisConfig(dataSourceCfg, logger)
	rClient := redis.NewClient(opts)
	namespace, ok := dataSourceCfg.Options[config.Namespace]
	if !ok || len(namespace) == 0 {
		namespace = defaultNamespace
	}
	mn := &RedisDataManager{
		logger: logger, redisOption: opts, redisClient: rClient, ctx: context.Background(),
		namespace: namespace,
	}
	return mn, nil
}

// buildRedisConfig builds redis.Options from map of values by known in config package set of keys
func buildRedisConfig(dataSourceCfd *config.DataSourceConfig, logger *logging.AppLogger) *redis.Options {
	dbNum, err := strconv.Atoi(dataSourceCfd.Options[config.DbNumber])
	if err != nil {
		logger.Error(sf.Format("can't be because we already called Validate(), but in any case: parsing error: {0}", err.Error()))
		return nil
	}
	opts := redis.Options{
		Addr: dataSourceCfd.Source,
		DB:   dbNum,
	}
	// passing credentials if we have it
	if dataSourceCfd.Credentials != nil {
		opts.Username = dataSourceCfd.Credentials.Username
		opts.Password = dataSourceCfd.Credentials.Password
	}
	// passing TLS if we have it
	val, ok := dataSourceCfd.Options[config.UseTls]
	if ok {
		useTls, parseErr := strconv.ParseBool(val)
		if parseErr == nil && useTls {
			opts.TLSConfig = &tls.Config{}
			val, ok = dataSourceCfd.Options[config.InsecureTls]
			if ok {
				inSecTls, parseInSecValErr := strconv.ParseBool(val)
				if parseInSecValErr == nil {
					opts.TLSConfig.InsecureSkipVerify = inSecTls
				}
			}
		}
	}

	return &opts
}

// TODO(SIA) add function keyExists
// TODO(SIA) Add a function to delete multiple keys at once

// upsertRedisString - inserting or updating a value by key
/* If there is no key, a key-value will be created. If the key is present, the value will be updated
 * Arguments:
 *    - objName - for logger
 *    - objKey - key object in redis
 *	  - objValue - new string value
 * Returns: error
 */
func (mn *RedisDataManager) upsertRedisString(objName objectType, objKey string, objValue string) error {
	statusCmd := mn.redisClient.Set(mn.ctx, objKey, objValue, 0)
	if statusCmd.Err() != nil {
		mn.logger.Warn(sf.Format("An error occurred during Set {0}: \"{1}\" from Redis server", objName, objKey))
		return statusCmd.Err()
	}
	return nil
}

// deleteRedisObject - delete key
/* Returns an error, if 0 items are deleted
 * Arguments:
 *    - objName - type of object = resource or table (basically is using for a logger)
 *    - objKey - key object in redis
 * Returns: error
 */
func (mn *RedisDataManager) deleteRedisObject(objName objectType, objKey string) error {
	redisIntCmd := mn.redisClient.Del(mn.ctx, objKey)
	if redisIntCmd.Err() != nil {
		mn.logger.Warn(sf.Format("An error occurred during Del {0}: \"{1}\" from Redis server", objName, objKey))
		return redisIntCmd.Err()
	}
	res := redisIntCmd.Val()
	if res == 0 {
		mn.logger.Warn(sf.Format("An error occurred during Del, 0 items deleted {0}: \"{1}\" from Redis server", objName, objKey))
		return errors.NewObjectNotFoundError(string(objName), objKey, "")
	}
	return nil
}

// deleteRedisObject - delete key
/* Returns an error, if 0 items are deleted
 * Arguments:
 *    - objName - type of object = resource or table (basically is using for a logger)
 *    - objKey - key object in redis
 *    - value - object value to remove
 * Returns: error
 */
func (mn *RedisDataManager) deleteRedisListItem(objName objectType, objKey string, value string) error {
	redisIntCmd := mn.redisClient.LRem(mn.ctx, objKey, 1, value)
	//.Del(mn.ctx, objKey)
	if redisIntCmd.Err() != nil {
		mn.logger.Warn(sf.Format("An error occurred during Del {0}: \"{1}\" from Redis server", objName, objKey))
		return redisIntCmd.Err()
	}
	res := redisIntCmd.Val()
	if res == 0 {
		mn.logger.Warn(sf.Format("An error occurred during Del, 0 items deleted {0}: \"{1}\" from Redis server", objName, objKey))
		return errors.NewObjectNotFoundError(string(objName), objKey, "")
	}
	return nil
}

// appendStringToRedisList - inserts a string into the list
/* Adds a row to the list. Internally, it uses RPush
 * Arguments:
 *    - objName - for logger
 *    - objKey - key object in redis
 *    - objValue - new string value
 * Returns: error
 */
func (mn *RedisDataManager) appendStringToRedisList(objName objectType, objKey string, objValue string) error {
	redisIntCmd := mn.redisClient.RPush(mn.ctx, objKey, objValue)
	if redisIntCmd.Err() != nil {
		mn.logger.Warn(sf.Format("An error occurred during RPush {0}: \"{1}\" from Redis server", objName, objKey))
		return redisIntCmd.Err()
	}
	return nil
}

// getSingleRedisObject is a method that DOESN'T work with List type object, only a String object type.
func getSingleRedisObject[T any](redisClient *redis.Client, ctx context.Context, logger *logging.AppLogger,
	objName objectType, objKey string,
) (*T, error) {
	redisCmd := redisClient.Get(ctx, objKey)
	if redisCmd.Err() != nil {
		logger.Warn(sf.Format("An error occurred during fetching {0}: \"{1}\" from Redis server", objName, objKey))
		if redisCmd.Err() == redis.Nil {
			return nil, errors.NewObjectNotFoundError(string(objName), objKey, "")
		}
		return nil, redisCmd.Err()
	}

	var obj T
	jsonBin := []byte(redisCmd.Val())
	err := json.Unmarshal(jsonBin, &obj)
	if err != nil {
		logger.Error(sf.Format("An error occurred during unmarshall {0} : \"{1}\"", objName, objKey))
		return nil, fmt.Errorf("json.Unmarshal failed: %w", err)
	}
	return &obj, nil
}

// getMultipleRedisObjects is a method that DOESN'T work with List type object, only a String object type
// Does not return an error if the object is not found
func getMultipleRedisObjects[T any](redisClient *redis.Client, ctx context.Context, logger *logging.AppLogger,
	objName objectType, objKey []string,
) ([]T, error) {
	redisCmd := redisClient.MGet(ctx, objKey...)
	if redisCmd.Err() != nil {
		// todo(UMV): print when this will be done https://github.com/Wissance/stringFormatter/issues/14
		logger.Warn(sf.Format("An error occurred during fetching {0}: from Redis server", objName))
		return nil, redisCmd.Err()
	}

	raw := redisCmd.Val()
	result := make([]T, len(raw))
	var unMarshalledRaw interface{}
	for i, v := range raw {
		err := json.Unmarshal([]byte(v.(string)), &unMarshalledRaw)
		if err != nil {
			logger.Error(sf.Format("An error occurred during unmarshall {0} : \"{1}\"", objName, objKey))
			return nil, fmt.Errorf("json.Unmarshal failed: %w", err)
		}
		result[i] = unMarshalledRaw.(T)
	}
	return result, nil
}

// getObjectsListOfSlicesItemsFromRedis functions returns object that stored as a LIST Object type every item of LIST is a slice
/* This function attempts to get Redis LIST object, if there are no objects in list return an error of type errors.ErrZeroLength
 * Every item of LIST is a SLICE of T -> []T
 * Parameters:
 * - redisClient - client to access Redis database
 * - ctx - go context
 * - logger - logger
 * - objName - name of resource (table)
 * - objKey - name of a list
 * Returns: slice []T and error
 */
func getObjectsListOfSlicesItemsFromRedis[T any](redisClient *redis.Client, ctx context.Context, logger *logging.AppLogger,
	objName objectType, objKey string,
) ([]T, error) {
	redisCmd := redisClient.LRange(ctx, objKey, 0, -1)
	if redisCmd.Err() != nil {
		logger.Warn(sf.Format("An error occurred during fetching {0}: \"{1}\" from Redis server", objName, objKey))
		return nil, redisCmd.Err()
	}

	// var obj T
	items := redisCmd.Val()
	if len(items) == 0 {
		logger.Warn(sf.Format("Received zero list items {0}: \"{1}\" from Redis server", objName, objKey))
		return nil, errors.ErrZeroLength
	}
	var result []T
	var portion []T
	for _, rawVal := range items {
		jsonBin := []byte(rawVal)
		err := json.Unmarshal(jsonBin, &portion) // already contains all SLICE in one object
		if err != nil {
			logger.Error(sf.Format("An error occurred during unmarshall {0} : \"{1}\", err: ", objName, objKey, err.Error()))
			return nil, err
		}
		result = append(result, portion...)
	}
	return result, nil
}

// getObjectsListOfNonSlicesItemsFromRedis functions gets object that stored as a LIST Object type every item is a single object
/* This function attempts to get Redis LIST object, if there are no objects in list return an error of type errors.ErrZeroLength
 * Every item of LIST is a T
 * Parameters:
 * - redisClient - client to access Redis database
 * - ctx - go context
 * - logger - logger
 * - objName - name of resource (table)
 * - objKey - name of a list
 * Returns: slice []T and error
 */
func getObjectsListOfNonSlicesItemsFromRedis[T any](redisClient *redis.Client, ctx context.Context, logger *logging.AppLogger,
	objName objectType, objKey string,
) ([]T, error) {
	redisCmd := redisClient.LRange(ctx, objKey, 0, -1)
	if redisCmd.Err() != nil {
		logger.Warn(sf.Format("An error occurred during fetching {0}: \"{1}\" from Redis server", objName, objKey))
		return nil, redisCmd.Err()
	}

	// var obj T
	items := redisCmd.Val()
	if len(items) == 0 {
		logger.Warn(sf.Format("Received zero list items {0}: \"{1}\" from Redis server", objName, objKey))
		return nil, errors.ErrZeroLength
	}
	var result []T
	var portion T
	for _, rawVal := range items {
		jsonBin := []byte(rawVal)
		err := json.Unmarshal(jsonBin, &portion) // already contains all SLICE in one object
		if err != nil {
			logger.Error(sf.Format("An error occurred during unmarshall {0} : \"{1}\", err: ", objName, objKey, err.Error()))
			return nil, err
		}
		result = append(result, portion)
	}
	return result, nil
}

func updateObjectListItemInRedis[T any](redisClient *redis.Client, ctx context.Context, logger *logging.AppLogger,
	objName objectType, objKey string, index int64, item T,
) error {
	redisCmd := redisClient.LSet(ctx, objKey, index, item)
	if redisCmd.Err() != nil {
		logger.Warn(sf.Format("An error occurred during setting (update) item in LIST with key: \"{0}\" of type \"{1}\" with index {2}, error: {3}",
			objKey, objName, index, redisCmd.Err()))
		return errors.NewUnknownError("LSet", "updateObjectListItemInRedis", redisCmd.Err())
	}

	return nil
}
