package files

import (
	"encoding/json"
	"os"

	"github.com/wissance/Ferrum/config"
	"github.com/wissance/Ferrum/utils/encoding"

	"github.com/wissance/Ferrum/errors"

	"github.com/google/uuid"
	"github.com/wissance/Ferrum/data"
	"github.com/wissance/Ferrum/logging"
	sf "github.com/wissance/stringFormatter"
)

type objectType string

const (
	Realm  objectType = "realm"
	Client objectType = "client"
	User   objectType = "user"
)

// FileDataManager is the simplest Data Storage without any dependencies, it uses single JSON file (it is users and clients RO auth server)
// This context type is extremely useful for simple systems
type FileDataManager struct {
	dataFile   string
	serverData data.ServerData
	logger     *logging.AppLogger
}

// CreateFileDataManagerWithInitData initializes instance of FileDataManager and sets loaded data to serverData
/* This factory function creates initialize with data instance of  FileDataManager, error reserved for usage but always nil here
 * Parameters:
 *    serverData already loaded data.ServerData from Json file in memory
 * Returns: context and error (currently is nil)
 */
func CreateFileDataManagerWithInitData(serverData *data.ServerData) (*FileDataManager, error) {
	// todo(UMV): todo provide an error handling
	mn := &FileDataManager{serverData: *serverData}
	return mn, nil
}

func CreateFileDataManager(dataFile string, logger *logging.AppLogger) (*FileDataManager, error) {
	// todo(UMV): todo provide an error handling
	mn := &FileDataManager{dataFile: dataFile, logger: logger}
	if err := mn.loadData(); err != nil {
		return nil, errors.NewUnknownError("data loading", "CreateFileDataManager", err)
	}
	return mn, nil
}

// IsAvailable methods that checks whether DataContext could be used or not
/* Availability means that serverData is not empty, so simple
 * Parameters: no
 * Returns true if DataContext is available
 */
func (mn *FileDataManager) IsAvailable() bool {
	if len(mn.dataFile) > 0 {
		_, err := os.Stat(mn.dataFile)
		return err == nil
	}
	return len(mn.serverData.Realms) > 0
}

// GetRealm function for getting Realm by name
/* Searches for a realm with name realmName in serverData adn return it. Returns realm with clients but no users
 * Parameters:
 *     - realmName - name of a realm
 * Returns: Realm and error
 */
func (mn *FileDataManager) GetRealm(realmName string) (*data.Realm, error) {
	if !mn.IsAvailable() {
		return nil, errors.NewDataProviderNotAvailable(string(config.FILE), mn.dataFile)
	}
	for _, e := range mn.serverData.Realms {
		// case-sensitive comparison, myapp and MyApP are different realms
		if e.Name == realmName {
			e.Users = nil
			e.Encoder = encoding.NewPasswordJsonEncoder(e.PasswordSalt)
			return &e, nil
		}
	}
	return nil, errors.NewObjectNotFoundError(string(Realm), realmName, "")
}

// GetUsers function for getting all Realm User
/* This function get realm by name ant extract all its users
 * Parameters:
 *     - realmName - name of a realm
 * Returns: slice of users and error
 */
func (mn *FileDataManager) GetUsers(realmName string) ([]data.User, error) {
	if !mn.IsAvailable() {
		return nil, errors.NewDataProviderNotAvailable(string(config.FILE), mn.dataFile)
	}
	for _, e := range mn.serverData.Realms {
		// case-sensitive comparison, myapp and MyApP are different realms
		if e.Name == realmName {
			if len(e.Users) == 0 {
				return nil, errors.ErrZeroLength
			}
			users := make([]data.User, len(e.Users))
			for i, u := range e.Users {
				user := data.CreateUser(u, nil)
				users[i] = user
			}
			return users, nil
		}
	}
	return nil, errors.NewObjectNotFoundError(string(User), "", sf.Format("get realm: {0} users", realmName))
}

// GetClient function for getting Realm Client by name
/* Searches for a client with name realmName in a realm. This function must be used after Realm was found.
 * Parameters:
 *     - realmName - realm containing clients to search
 *     - clientName - name of a client
 * Returns: Client and error
 */
func (mn *FileDataManager) GetClient(realmName string, clientName string) (*data.Client, error) {
	if !mn.IsAvailable() {
		return nil, errors.NewDataProviderNotAvailable(string(config.FILE), mn.dataFile)
	}
	realm, err := mn.GetRealm(realmName)
	if err != nil {
		mn.logger.Warn(sf.Format("GetRealm failed: {0}", err.Error()))
		return nil, err
	}

	for _, c := range realm.Clients {
		if c.Name == clientName {
			return &c, nil
		}
	}
	return nil, errors.NewObjectNotFoundError(string(Client), clientName, sf.Format("realm: {0}", realmName))
}

// GetUser function for getting Realm User by userName
/* Searches for a user with specified name in a realm.  This function must be used after Realm was found.
 * Parameters:
 *     - realmName - realm containing users to search
 *     - userName - name of a user
 * Returns: User and error
 */
func (mn *FileDataManager) GetUser(realmName string, userName string) (data.User, error) {
	if !mn.IsAvailable() {
		return data.User(nil), errors.NewDataProviderNotAvailable(string(config.FILE), mn.dataFile)
	}
	users, err := mn.GetUsers(realmName)
	if err != nil {
		mn.logger.Warn(sf.Format("GetUsers failed: {0}", err.Error()))
		return nil, err
	}
	for _, u := range users {
		if u.GetUsername() == userName {
			return u, nil
		}
	}
	return nil, errors.NewObjectNotFoundError(string(User), userName, sf.Format("realm: {0}", realmName))
}

// GetUserById function for getting Realm User by UserId (uuid)
/* same functions as GetUser but uses userId to search instead of username, works by sequential iteration
 */
func (mn *FileDataManager) GetUserById(realmName string, userId uuid.UUID) (data.User, error) {
	if !mn.IsAvailable() {
		return data.User(nil), errors.NewDataProviderNotAvailable(string(config.FILE), mn.dataFile)
	}
	users, err := mn.GetUsers(realmName)
	if err != nil {
		mn.logger.Warn(sf.Format("GetUsers failed: {0}", err.Error()))
		return nil, err
	}
	for _, u := range users {
		if u.GetId() == userId {
			return u, nil
		}
	}
	return nil, errors.NewObjectNotFoundError(string(User), userId.String(), sf.Format("realm: {0}", realmName))
}

// CreateRealm creates new data.Realm in a data store, receive realmData unmarshalled json in a data.Realm
/*
 *
 */
func (mn *FileDataManager) CreateRealm(realmData data.Realm) error {
	return errors.ErrOperationNotImplemented
}

// CreateClient creates new data.Client in a data store, requires to pass realmName (because client name is not unique), clientData is an unmarshalled json of type data.Client
func (mn *FileDataManager) CreateClient(realmName string, clientData data.Client) error {
	return errors.ErrOperationNotImplemented
}

// CreateUser creates new data.User in a data store within a realm with name = realmName
func (mn *FileDataManager) CreateUser(realmName string, userData data.User) error {
	return errors.ErrOperationNotImplemented
}

// UpdateRealm updates existing data.Realm in a data store within name = realmData, and new data = realmData
func (mn *FileDataManager) UpdateRealm(realmName string, realmData data.Realm) error {
	return errors.ErrOperationNotImplemented
}

// UpdateClient updates existing data.Client in a data store with name = clientName and new data = clientData
func (mn *FileDataManager) UpdateClient(realmName string, clientName string, clientData data.Client) error {
	return errors.ErrOperationNotImplemented
}

// UpdateUser updates existing data.User in a data store with realm name = realName, username = userName and data=userData
func (mn *FileDataManager) UpdateUser(realmName string, userName string, userData data.User) error {
	return errors.ErrOperationNotImplemented
}

// DeleteRealm removes realm from data storage (Should be a CASCADE remove of all related Users and Clients)
func (mn *FileDataManager) DeleteRealm(realmName string) error {
	return errors.ErrOperationNotImplemented
}

// DeleteClient removes client with name = clientName from realm with name = clientName
func (mn *FileDataManager) DeleteClient(realmName string, clientName string) error {
	return errors.ErrOperationNotImplemented
}

// DeleteUser removes data.User from data store by user (userName) and realm (realmName) name respectively
func (mn *FileDataManager) DeleteUser(realmName string, userName string) error {
	return errors.ErrOperationNotImplemented
}

func (mn *FileDataManager) GetUserFederationConfig(realmName string, configName string) (*data.UserFederationServiceConfig, error) {
	return nil, errors.ErrOperationNotImplemented
}

func (mn *FileDataManager) CreateUserFederationConfig(realmName string, userFederationConfig data.UserFederationServiceConfig) error {
	return errors.ErrOperationNotImplemented
}

func (mn *FileDataManager) UpdateUserFederationConfig(realmName string, configName string, userFederationConfig data.UserFederationServiceConfig) error {
	return errors.ErrOperationNotImplemented
}

func (mn *FileDataManager) DeleteUserFederationConfig(realmName string, configName string) error {
	return errors.ErrOperationNotImplemented
}

// loadData this function loads data from JSON file (dataFile) to serverData
func (mn *FileDataManager) loadData() error {
	rawData, err := os.ReadFile(mn.dataFile)
	if err != nil {
		mn.logger.Error(sf.Format("An error occurred during config file reading: {0}", err.Error()))
		return errors.NewUnknownError("os.ReadFile", "FileDataManager.loadData", err)
	}
	mn.serverData = data.ServerData{}
	if err = json.Unmarshal(rawData, &mn.serverData); err != nil {
		mn.logger.Error(sf.Format("An error occurred during data file unmarshal: {0}", err.Error()))
		return errors.NewUnknownError("json.Unmarshal", "FileDataManager.loadData", err)
	}

	return nil
}
