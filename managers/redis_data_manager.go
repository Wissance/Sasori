package managers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/wissance/Ferrum/config"
	"github.com/wissance/Ferrum/data"
	"github.com/wissance/Ferrum/logging"
	sf "github.com/wissance/stringFormatter"
	"strconv"
)

const (
	realmCollection         = "realms"
	clientsCollection       = "clients"
	usersCollection         = "users"
	userKeyTemplate         = "fe_user_{0}"
	realmKeyTemplate        = "fe_realm_{0}"
	realmClientsKeyTemplate = "fe_realm_{0}_clients"
	clientKeyTemplate       = "fe_client_{0}"
	realmUsersKeyTemplate   = "fe_realm_{0}_users"
)

type objectType string

const (
	Realm        objectType = "realm"
	RealmClients            = "realm clients"
	Client                  = "client"
	User                    = "user"
)

// RedisDataManager is a redis client
/*
 * Redis Data Manager is a service class for managing authorization server data in Redis
 * There are following store Rules:
 * 1. Realms (data.Realm) in Redis storing separately from Clients && Users, every Realm stores in Redis by key forming from template && Realm name
 *    i.e. if we have Realm with name "wissance" it could be accessed by key fe_realm_wissance (realmKeyTemplate)
 * 2. Realm Clients ([]uuid.UUID) storing in Redis by key forming from template, Realm with name wissance has array of clients is by key
 *    fe_realm_wissance_clients (realmClientsKeyTemplate)
 * 3. Every Client (data.Client) stores separately by key forming from client id (different realms could have clients with same name but in different realm,
 *    Client Name is unique only in Realm) and template clientKeyTemplate, therefore realm
 */
type RedisDataManager struct {
	redisOption *redis.Options
	redisClient *redis.Client
	logger      *logging.AppLogger
	ctx         context.Context
}

func CreateRedisDataManager(dataSourceCfd *config.DataSourceConfig, logger *logging.AppLogger) DataContext {
	opts := buildRedisConfig(dataSourceCfd, logger)
	rClient := redis.NewClient(opts)
	mn := &RedisDataManager{logger: logger, redisOption: opts, redisClient: rClient, ctx: context.Background()}
	dc := DataContext(mn)
	return dc
}

func (mn *RedisDataManager) GetRealm(realmName string) *data.Realm {
	realmKey := sf.Format(realmKeyTemplate, realmName)
	realm := getObjectFromRedis[data.Realm](mn.redisClient, mn.ctx, mn.logger, Realm, realmKey)
	return realm
}

func (mn *RedisDataManager) GetClient(realm *data.Realm, name string) *data.Client {
	clientKey := sf.Format(clientKeyTemplate, name)
	// todo (UMV): change order query realms client first ...
	client := getObjectFromRedis[data.Client](mn.redisClient, mn.ctx, mn.logger, Client, clientKey)
	if client == nil {
		return client
	}
	realmClientsKey := sf.Format(realmClientsKeyTemplate, realm.Name)
	// realm_%name%_clients contains array with configured clients ID (uuid.UUID) for that realm
	realmClients := getObjectFromRedis[[]uuid.UUID](mn.redisClient, mn.ctx, mn.logger, RealmClients, realmClientsKey)
	if realmClients == nil {
		mn.logger.Error(sf.Format("There are no clients for realm: \"{0} \" in Redis", realm.Name))
		return nil
	}
	for _, rc := range *realmClients {
		if rc == client.ID {
			return client
		}
	}
	return nil
}

func (mn *RedisDataManager) GetUser(realm *data.Realm, userName string) *data.User {
	userKey := sf.Format(userKeyTemplate, userName)
	rawUser := getObjectFromRedis[interface{}](mn.redisClient, mn.ctx, mn.logger, User, userKey)
	user := data.CreateUser(rawUser)
	// todo (UMV): check that client is from Realm, get another obj - realmClientsKeyTemplate
	return &user
}

func (mn *RedisDataManager) GetUserById(realm *data.Realm, userId uuid.UUID) *data.User {
	return nil
}

func (mn *RedisDataManager) GetRealmUsers(realmName string) *[]data.User {
	return nil
}

func getObjectFromRedis[T any](redisClient *redis.Client, ctx context.Context, logger *logging.AppLogger,
	objName objectType, objKey string) *T {
	redisCmd := redisClient.Get(ctx, objKey)
	if redisCmd.Err() != nil {
		logger.Warn(sf.Format("An error occurred during fetching {0}: \"{1}\" from Redis server", objName, objKey))
		return nil
	}

	var obj T
	realmJson := []byte(redisCmd.Val())
	err := json.Unmarshal(realmJson, &obj)
	if err != nil {
		logger.Error(sf.Format("An error occurred during {0} : \"{1}\" unmarshall", objName, objKey))
	}
	return &obj
}

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
