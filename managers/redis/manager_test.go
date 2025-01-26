package redis

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wissance/Ferrum/config"
	"github.com/wissance/Ferrum/data"
	appErrs "github.com/wissance/Ferrum/errors"
	"github.com/wissance/Ferrum/logging"
	"github.com/wissance/Ferrum/utils/encoding"
	sf "github.com/wissance/stringFormatter"
)

const (
	testUser         = "ferrum_db"
	testUserPassword = "FeRRuM000"
	testRedisSource  = "127.0.0.1:6379"
)

func TestCreateRealmSuccessfully(t *testing.T) {
	testCases := []struct {
		name              string
		realmNameTemplate string
		clients           []string
		users             []string
	}{
		{name: "realm_without_clients", realmNameTemplate: "app1_test_{0}", clients: []string{}, users: []string{}},
		{name: "realm_with_one_client", realmNameTemplate: "app2_test_{0}", clients: []string{"app_client2"}, users: []string{}},
		{name: "realm_with_one_client_and_one_user", realmNameTemplate: "app3_test_{0}", clients: []string{"app_client3"}, users: []string{"app_user3"}},
	}

	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			manager := createTestRedisDataManager(t)
			realm := data.Realm{
				Name:                   sf.Format(tCase.realmNameTemplate, uuid.New().String()),
				TokenExpiration:        3600,
				RefreshTokenExpiration: 1800,
			}

			for _, c := range tCase.clients {
				client := data.Client{
					Name: c,
					Type: data.Public,
					ID:   uuid.New(),
					Auth: data.Authentication{
						Type:  data.ClientIdAndSecrets,
						Value: uuid.New().String(),
					},
				}
				realm.Clients = append(realm.Clients, client)
			}

			for _, u := range tCase.users {
				userJson := sf.Format(`{"info":{"preferred_username":"{0}"}}`, u)
				var rawUser interface{}
				err := json.Unmarshal([]byte(userJson), &rawUser)
				assert.NoError(t, err)
				realm.Users = append(realm.Users, rawUser)
			}

			err := manager.CreateRealm(realm)
			assert.NoError(t, err)
			r, err := manager.GetRealm(realm.Name)
			assert.NoError(t, err)
			checkRealm(t, &realm, r)
			assert.Equal(t, len(tCase.clients), len(r.Clients))
			users, err := manager.GetUsers(realm.Name)
			assert.NoError(t, err)
			assert.Equal(t, len(realm.Users), len(users))
			expectedUsers := make([]data.User, len(realm.Users))
			if len(realm.Users) > 0 {
				for i := range realm.Users {
					expectedUsers[i] = data.CreateUser(realm.Users[i], realm.Encoder)
				}
			}
			checkUsers(t, &expectedUsers, &users)
			err = manager.DeleteRealm(realm.Name)
			assert.NoError(t, err)
		})
	}
}

func TestCreateRealmWithFederationSuccessfully(t *testing.T) {
	testCases := []struct {
		name                  string
		realmNameTemplate     string
		federationServiceName string
		clients               []string
		users                 []string
	}{
		{
			name: "realm_with_two_fed_service", realmNameTemplate: "app_with_fed_test_{0}", federationServiceName: "test_ldap",
			clients: []string{}, users: []string{},
		},
	}

	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			manager := createTestRedisDataManager(t)
			realm := data.Realm{
				Name:                   sf.Format(tCase.realmNameTemplate, uuid.New().String()),
				TokenExpiration:        3600,
				RefreshTokenExpiration: 1800,
				UserFederationServices: []data.UserFederationServiceConfig{
					{
						Name: tCase.federationServiceName,
						Type: data.LDAP,
						Url:  "ldap://ldap.wissance.com:389",
					},
					{
						Name: tCase.federationServiceName + "_2",
						Type: data.LDAP,
						Url:  "ldap://ldap2.wissance.com:389",
					},
				},
			}
			err := manager.CreateRealm(realm)
			assert.NoError(t, err)
			r, err := manager.GetRealm(realm.Name)
			assert.NoError(t, err)
			checkRealm(t, &realm, r)
			err = manager.DeleteRealm(realm.Name)
			assert.NoError(t, err)
			userFederationConfigs, err := manager.GetUserFederationConfigs(realm.Name)
			assert.ErrorIs(t, err, appErrs.ErrZeroLength)
			assert.Nil(t, userFederationConfigs)
		})
	}
}

func TestCreateRealmFailsDuplicateRealm(t *testing.T) {
	manager := createTestRedisDataManager(t)
	realm := data.Realm{
		Name:                   "realm_for_duplicate_check",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}

	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	err = manager.CreateRealm(realm)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.ErrExists))

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestUpdateRealmSuccessfully(t *testing.T) {
	// 1. Create Realm
	manager := createTestRedisDataManager(t)
	realm := data.Realm{
		Name:                   sf.Format("realm_4_update_check_{0}", uuid.New().String()),
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)
	r, err := manager.GetRealm(realm.Name)
	assert.NoError(t, err)
	checkRealm(t, &realm, r)
	prevRealmName := realm.Name
	// 2. Update Realm
	realm.Name = sf.Format("realm_4_update_check_{0}_new_realm_name", uuid.New().String())
	realm.TokenExpiration = 5400
	client := data.Client{
		Name: "app_4_update_realm",
		Type: data.Public,
		ID:   uuid.New(),
		Auth: data.Authentication{
			Type:  data.ClientIdAndSecrets,
			Value: uuid.New().String(),
		},
	}
	realm.Clients = append(realm.Clients, client)

	userJson := sf.Format(`{"info":{"preferred_username":"{0}"}}`, "new_app_user")
	var rawUser interface{}
	err = json.Unmarshal([]byte(userJson), &rawUser)
	assert.NoError(t, err)
	realm.Users = append(realm.Users, rawUser)

	err = manager.UpdateRealm(prevRealmName, realm)
	assert.NoError(t, err)
	r, err = manager.GetRealm(realm.Name)
	assert.NoError(t, err)
	checkRealm(t, &realm, r)
	// 3. Delete Realm
	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestUpdateRealmFailsNonExistingRealm(t *testing.T) {
	manager := createTestRedisDataManager(t)
	realm := data.Realm{
		Name: "super-duper-realm",
	}
	nonExistingRealm := sf.Format("non_existing_{0}", uuid.New().String())
	err := manager.UpdateRealm(nonExistingRealm, realm)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.EmptyNotFoundErr))
}

func TestDeleteRealmFailsNonExistingRealm(t *testing.T) {
	manager := createTestRedisDataManager(t)

	nonExistingRealm := sf.Format("non_existing_{0}", uuid.New().String())
	err := manager.DeleteRealm(nonExistingRealm)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.EmptyNotFoundErr))
}

func TestGetRealmFailsNonExistingRealm(t *testing.T) {
	manager := createTestRedisDataManager(t)

	nonExistingRealm := sf.Format("non_existing_{0}", uuid.New().String())
	_, err := manager.GetRealm(nonExistingRealm)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.EmptyNotFoundErr))
}

func TestGetClientsSuccessfully(t *testing.T) {
	// 1. Create Realm
	manager := createTestRedisDataManager(t)
	realm := data.Realm{
		Name:                   sf.Format("realm_4_get_multiple_clients_{0}", uuid.New().String()),
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)
	// 2. Create multiple clients
	clients := make([]data.Client, 3)
	for i := range clients {
		// create && store
		clients[i] = data.Client{
			Name: sf.Format("client_{0}_test_multiple_client_get_{1}", i, uuid.New().String()),
			Type: data.Public,
		}
		err = manager.CreateClient(realm.Name, clients[i])
		assert.NoError(t, err)
	}
	// 3. Get all related to realm clients
	c, err := manager.GetClients(realm.Name)
	checkClients(t, &clients, &c)
	assert.NoError(t, err)
	// 4. Cleanup via Realm Delete
	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestGetClientsSuccessfullyForEmptyRealm(t *testing.T) {
	// 1. Create Realm
	manager := createTestRedisDataManager(t)
	realm := data.Realm{
		Name:                   sf.Format("empty_realm_4_get_multiple_clients_{0}", uuid.New().String()),
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)
	// 3. Get all related to realm clients
	c, err := manager.GetClients(realm.Name)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(c))
	// 4. Cleanup via Realm Delete
	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestGetClientsSuccessfullyRealmNotExist(t *testing.T) {
	manager := createTestRedisDataManager(t)
	nonExistingRealm := sf.Format("non_existing_{0}", uuid.New().String())
	c, err := manager.GetClients(nonExistingRealm)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(c))
}

func TestCreateClientSuccessfully(t *testing.T) {
	testCases := []struct {
		name       string
		clientName string
		clientType data.ClientType
	}{
		{name: "create_public_client", clientName: "sample_pub_client", clientType: data.Public},
		{name: "create_conf_client", clientName: "sample_conf_client", clientType: data.Confidential},
	}
	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			manager := createTestRedisDataManager(t)
			realm := data.Realm{
				Name:                   "sample_realm_4_create_client_tests",
				TokenExpiration:        3600,
				RefreshTokenExpiration: 1800,
			}
			err := manager.CreateRealm(realm)
			assert.NoError(t, err)
			_, err = manager.GetRealm(realm.Name)
			assert.NoError(t, err)

			client := data.Client{
				Name: "app_4_update_realm",
				Type: tCase.clientType,
				ID:   uuid.New(),
			}
			if client.Type == data.Confidential {
				client.Auth = data.Authentication{
					Type:  data.ClientIdAndSecrets,
					Value: uuid.New().String(),
				}
			}

			err = manager.CreateClient(realm.Name, client)
			assert.NoError(t, err)

			c, err := manager.GetClient(realm.Name, client.Name)
			assert.NoError(t, err)
			checkClient(t, &client, c)

			err = manager.DeleteRealm(realm.Name)
			assert.NoError(t, err)
		})
	}
}

func TestCreateClientFailsDuplicateClient(t *testing.T) {
	manager := createTestRedisDataManager(t)
	realm := data.Realm{
		Name:                   "sample_realm_4_check_client_duplicate",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	client := data.Client{
		Name: "app_4_check_duplicate_client_create",
		Type: data.Public,
		ID:   uuid.New(),
	}

	err = manager.CreateClient(realm.Name, client)
	assert.NoError(t, err)

	err = manager.CreateClient(realm.Name, client)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.ErrExists))

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestUpdateClientSuccessfully(t *testing.T) {
	manager := createTestRedisDataManager(t)
	realm := data.Realm{
		Name:                   "sample_realm_4_test_client_update",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	client := data.Client{
		Name: "app_4_check_client_update",
		Type: data.Public,
		ID:   uuid.New(),
	}

	err = manager.CreateClient(realm.Name, client)
	assert.NoError(t, err)

	c, err := manager.GetClient(realm.Name, client.Name)
	assert.NoError(t, err)
	checkClient(t, &client, c)

	client.Auth = data.Authentication{
		Type:  data.ClientIdAndSecrets,
		Value: uuid.New().String(),
	}

	err = manager.UpdateClient(realm.Name, client.Name, client)
	assert.NoError(t, err)
	c, err = manager.GetClient(realm.Name, client.Name)
	assert.NoError(t, err)
	checkClient(t, &client, c)

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestUpdateClientFailsNonExistingClient(t *testing.T) {
	manager := createTestRedisDataManager(t)
	realm := data.Realm{
		Name:                   "sample_realm_4_test_non_existing_client_update",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	nonExistingClient := sf.Format("non-existing-client_{0}", uuid.New().String())
	client := data.Client{
		Name: "Surprise Motherfucker",
	}

	err = manager.UpdateClient(realm.Name, nonExistingClient, client)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.EmptyNotFoundErr))

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestDeleteClientFailsNonExistingClient(t *testing.T) {
	manager := createTestRedisDataManager(t)
	realm := data.Realm{
		Name:                   "sample_realm_4_test_non_existing_client_update",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	nonExistingClient := sf.Format("non-existing-client_{0}", uuid.New().String())

	err = manager.DeleteClient(realm.Name, nonExistingClient)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.EmptyNotFoundErr))

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestGetClientFailsNonExistingClient(t *testing.T) {
	manager := createTestRedisDataManager(t)
	realm := data.Realm{
		Name:                   "sample_realm_4_test_non_existing_client_get",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	nonExistingClient := sf.Format("non-existing-client_{0}", uuid.New().String())

	_, err = manager.GetClient(realm.Name, nonExistingClient)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.EmptyNotFoundErr))

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestGetUsersSuccessfully(t *testing.T) {
	// 1. Create Realm
	manager := createTestRedisDataManager(t)
	r := data.Realm{
		Name:                   sf.Format("realm_4_get_multiple_users_{0}", uuid.New().String()),
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(r)
	assert.NoError(t, err)
	realm, err := manager.GetRealm(r.Name)
	assert.NoError(t, err)
	// 2. Create multiple users
	users := make([]data.User, 3)
	for i := range users {
		userId := uuid.New().String()
		userName := sf.Format("test_user_{0}_{1}", i, userId)
		jsonTemplate := `{"info":{"sub":"{2}", "name":"{0}", "preferred_username": "{1}"}, "credentials":{"password": "123"}}`
		jsonStr := sf.Format(jsonTemplate, userName, userName, userId)
		var rawUser interface{}
		err = json.Unmarshal([]byte(jsonStr), &rawUser)
		assert.NoError(t, err)
		user := data.CreateUser(rawUser, nil)
		err = manager.CreateUser(realm.Name, user)
		users[i] = user
		assert.NoError(t, err)
	}
	// 3. Get all related to realm users
	u, err := manager.GetUsers(realm.Name)
	checkUsers(t, &users, &u)
	assert.NoError(t, err)
	// 4. Cleanup via Realm Delete
	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestGetUsersSuccessfullyEmptyRealm(t *testing.T) {
	// 1. Create Realm
	manager := createTestRedisDataManager(t)
	realm := data.Realm{
		Name:                   sf.Format("empty_realm_4_get_multiple_users_{0}", uuid.New().String()),
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	// 2. Get all related to realm users
	u, err := manager.GetUsers(realm.Name)
	assert.Equal(t, 0, len(u))
	assert.NoError(t, err)

	// 3. Cleanup all resources via DeleteRealm
	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestGetUsersSuccessfullyNonExistingRealm(t *testing.T) {
	manager := createTestRedisDataManager(t)
	nonExistingRealm := sf.Format("non_existing_{0}", uuid.New().String())
	u, err := manager.GetUsers(nonExistingRealm)
	assert.Equal(t, 0, len(u))
	assert.NoError(t, err)
}

func TestGetUserByIdSuccessfully(t *testing.T) {
	manager := createTestRedisDataManager(t)
	// here we are going to create user separately from Realm via manager.CreateUser
	realm := data.Realm{
		Name:                   "realm_4_test_get_user_by_id",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	userId := uuid.New()
	jsonTemplate := `{"info":{"sub":"{2}", "name":"{0}", "preferred_username": "{0}"}, "credentials":{"password": "{1}"}}`
	jsonStr := sf.Format(jsonTemplate, "lipa", "321123", userId)
	var rawUser interface{}
	err = json.Unmarshal([]byte(jsonStr), &rawUser)
	assert.NoError(t, err)
	user := data.CreateUser(rawUser, nil)
	err = manager.CreateUser(realm.Name, user)
	assert.NoError(t, err)

	u, err := manager.GetUserById(realm.Name, userId)
	assert.NoError(t, err)
	checkUser(t, &user, &u)

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestGetUserByIdFailsUserDoesNotExists(t *testing.T) {
	manager := createTestRedisDataManager(t)
	// here we are going to create user separately from Realm via manager.CreateUser
	realm := data.Realm{
		Name:                   "realm_4_test_get_non_existing_user_by_id",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	userId := uuid.New()

	_, err = manager.GetUserById(realm.Name, userId)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.EmptyNotFoundErr))

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestCreateUserSuccessfully(t *testing.T) {
	testCases := []struct {
		name              string
		realmNameTemplate string
		userName          string
	}{
		{name: "create_min_user", realmNameTemplate: "app_realm_{0}", userName: "app_user"},
	}

	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			manager := createTestRedisDataManager(t)
			// here we are going to create user separately from Realm via manager.CreateUser
			realm := data.Realm{
				Name:                   sf.Format(tCase.realmNameTemplate, uuid.New().String()),
				TokenExpiration:        3600,
				RefreshTokenExpiration: 1800,
			}
			err := manager.CreateRealm(realm)
			assert.NoError(t, err)
			r, err := manager.GetRealm(realm.Name)
			assert.NoError(t, err)
			checkRealm(t, &realm, r)

			jsonTemplate := `{"info":{"name":"{0}", "preferred_username": "{1}"}, "credentials":{"password": "123"}}`
			jsonStr := sf.Format(jsonTemplate, tCase.userName, tCase.userName)
			var rawUser interface{}
			err = json.Unmarshal([]byte(jsonStr), &rawUser)
			assert.NoError(t, err)
			user := data.CreateUser(rawUser, r.Encoder)
			err = manager.CreateUser(realm.Name, user)
			assert.NoError(t, err)
			storedUser, err := manager.GetUser(realm.Name, tCase.userName)
			assert.NoError(t, err)
			assert.Equal(t, tCase.userName, storedUser.GetUsername())
			err = manager.DeleteRealm(realm.Name)
			assert.NoError(t, err)
		})
	}
}

func TestCreateUserFailsDuplicateUser(t *testing.T) {
	manager := createTestRedisDataManager(t)
	// here we are going to create user separately from Realm via manager.CreateUser
	realm := data.Realm{
		Name:                   "realm_4_test_user_create_fails_duplicate",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
		Encoder:                encoding.NewPasswordJsonEncoder("salt"),
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	jsonTemplate := `{"info":{"name":"{0}", "preferred_username": "{0}"}, "credentials":{"password": "{1}"}}`
	jsonStr := sf.Format(jsonTemplate, "iiivanov", "321_ne_314ras")
	var rawUser interface{}
	err = json.Unmarshal([]byte(jsonStr), &rawUser)
	assert.NoError(t, err)
	user := data.CreateUser(rawUser, realm.Encoder)
	err = manager.CreateUser(realm.Name, user)
	assert.NoError(t, err)

	err = manager.CreateUser(realm.Name, user)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.ErrExists))

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestUpdateUserSuccessfully(t *testing.T) {
	manager := createTestRedisDataManager(t)
	// here we are going to create user separately from Realm via manager.CreateUser
	realm := data.Realm{
		Name:                   "realm_4_test_user_update",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
		Encoder:                encoding.NewPasswordJsonEncoder("salt"),
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)
	userName := "pppetrov"

	jsonTemplate := `{"info":{"name":"{0}", "preferred_username": "{0}"}, "credentials":{"password": "{1}"}}`
	jsonStr := sf.Format(jsonTemplate, userName, "67890")
	var rawUser interface{}
	err = json.Unmarshal([]byte(jsonStr), &rawUser)
	assert.NoError(t, err)
	user := data.CreateUser(rawUser, realm.Encoder)
	err = manager.CreateUser(realm.Name, user)
	assert.NoError(t, err)

	jsonTemplate = `{"info":{"sub":"{2}", "name":"{0}", "preferred_username": "{0}"}, "credentials":{"password": "{1}"}}`
	jsonStr = sf.Format(jsonTemplate, "pppetrov", "67890", "00000000-0000-0000-0000-000000000001")
	err = json.Unmarshal([]byte(jsonStr), &rawUser)
	assert.NoError(t, err)
	user = data.CreateUser(rawUser, nil)

	err = manager.UpdateUser(realm.Name, userName, user)
	assert.NoError(t, err)
	u, err := manager.GetUser(realm.Name, userName)
	assert.NoError(t, err)
	checkUser(t, &user, &u)

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestUpdateUserFailsNonExistingUser(t *testing.T) {
	manager := createTestRedisDataManager(t)
	// here we are going to create user separately from Realm via manager.CreateUser
	realm := data.Realm{
		Name:                   "realm_4_test_user_update_fails_non_existing_user",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	userName := sf.Format("non-existing-user-{0}", uuid.New().String())
	jsonTemplate := `{"info":{"name":"{0}", "preferred_username": "{0}"}, "credentials":{"password": "{1}"}}`
	jsonStr := sf.Format(jsonTemplate, userName, "67890")
	var rawUser interface{}
	err = json.Unmarshal([]byte(jsonStr), &rawUser)
	assert.NoError(t, err)
	user := data.CreateUser(rawUser, nil)
	err = manager.UpdateUser(realm.Name, userName, user)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.EmptyNotFoundErr))

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestDeleteUserSuccessfully(t *testing.T) {
	manager := createTestRedisDataManager(t)
	// here we are going to create user separately from Realm via manager.CreateUser
	realm := data.Realm{
		Name:                   "realm_4_test_user_delete",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)
	userName := "sidorov"

	jsonTemplate := `{"info":{"name":"{0}", "preferred_username": "{0}"}, "credentials":{"password": "{1}"}}`
	jsonStr := sf.Format(jsonTemplate, userName, "98765")
	var rawUser interface{}
	err = json.Unmarshal([]byte(jsonStr), &rawUser)
	assert.NoError(t, err)
	user := data.CreateUser(rawUser, nil)
	err = manager.CreateUser(realm.Name, user)
	assert.NoError(t, err)
	u, err := manager.GetUser(realm.Name, userName)
	assert.NoError(t, err)
	checkUser(t, &user, &u)

	err = manager.DeleteUser(realm.Name, userName)
	assert.NoError(t, err)
	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestDeleteUserFailsNonExistingUser(t *testing.T) {
	manager := createTestRedisDataManager(t)
	// here we are going to create user separately from Realm via manager.CreateUser
	realm := data.Realm{
		Name:                   "realm_4_test_user_delete_fails_non_existing",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	userName := sf.Format("non_existing_user_{0}", uuid.New().String())
	err = manager.DeleteUser(realm.Name, userName)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.EmptyNotFoundErr))

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestGetUserFailsNonExistingUser(t *testing.T) {
	manager := createTestRedisDataManager(t)
	// here we are going to create user separately from Realm via manager.CreateUser
	realm := data.Realm{
		Name:                   "realm_4_test_user_delete_fails_non_existing",
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}
	err := manager.CreateRealm(realm)
	assert.NoError(t, err)

	userName := sf.Format("non_existing_user_{0}", uuid.New().String())
	_, err = manager.GetUser(realm.Name, userName)
	assert.Error(t, err)
	assert.True(t, errors.As(err, &appErrs.EmptyNotFoundErr))

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestChangeUserPasswordSuccessfully(t *testing.T) {
	manager := createTestRedisDataManager(t)
	// 1. Create Realm+Client+User
	realm := &data.Realm{
		Name:                   sf.Format("app_4_user_pwd_change_check_{0}", uuid.New().String()),
		TokenExpiration:        3600,
		RefreshTokenExpiration: 1800,
	}

	client := data.Client{
		Name: "app_client_4_check_pwd_change",
		Type: data.Public,
		ID:   uuid.New(),
		Auth: data.Authentication{
			Type:  data.ClientIdAndSecrets,
			Value: uuid.New().String(),
		},
	}
	realm.Clients = append(realm.Clients, client)
	err := manager.CreateRealm(*realm)
	assert.NoError(t, err)

	realm, err = manager.GetRealm(realm.Name)
	assert.NoError(t, err)

	userName := "new_app_user"
	userTemplate := `{"info":{"preferred_username":"{0}"}, "credentials":{"password": "{1}"}}`
	userJson := sf.Format(userTemplate, userName, "123")
	var rawUser interface{}
	err = json.Unmarshal([]byte(userJson), &rawUser)
	assert.NoError(t, err)
	user := data.CreateUser(rawUser, realm.Encoder)
	err = manager.CreateUser(realm.Name, user)
	assert.NoError(t, err)

	// 2. Reset Password and check ...
	newPassword := "123_ololo_321"
	err = manager.SetPassword(realm.Name, userName, newPassword)
	assert.NoError(t, err)

	var rawUser2 interface{}
	userJson = sf.Format(userTemplate, userName, newPassword)
	err = json.Unmarshal([]byte(userJson), &rawUser2)
	assert.NoError(t, err)
	expectedUser := data.CreateUser(rawUser2, realm.Encoder)
	assert.NoError(t, err)
	u, err := manager.GetUser(realm.Name, userName)
	assert.NoError(t, err)
	checkUser(t, &expectedUser, &u)

	err = manager.DeleteRealm(realm.Name)
	assert.NoError(t, err)
}

func TestCreateUserFederationServiceConfigSuccessfully(t *testing.T) {
	testCases := []struct {
		name                  string
		realmNameTemplate     string
		federationServiceName string
	}{
		{name: "realm_with_one_fed_service", realmNameTemplate: "app_with_fed_test_{0}", federationServiceName: "test_ldap"},
	}

	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			manager := createTestRedisDataManager(t)
			realm := data.Realm{
				Name:                   sf.Format(tCase.realmNameTemplate, uuid.New().String()),
				TokenExpiration:        3600,
				RefreshTokenExpiration: 1800,
			}

			err := manager.CreateRealm(realm)
			assert.NoError(t, err)
			r, err := manager.GetRealm(realm.Name)
			assert.NoError(t, err)
			checkRealm(t, &realm, r)

			// Creation of sample UserFederationService
			userFederationServiceConfig := data.UserFederationServiceConfig{
				Name:        tCase.federationServiceName,
				Url:         "ldap://testldap.wissance.com:389",
				Type:        data.LDAP,
				SysUser:     "admin",
				SysPassword: "admin",
			}

			err = manager.CreateUserFederationConfig(realm.Name, userFederationServiceConfig)
			assert.NoError(t, err)

			actualConfig, err := manager.GetUserFederationConfig(realm.Name, userFederationServiceConfig.Name)
			assert.NoError(t, err)
			checkUserFederationConfig(t, &userFederationServiceConfig, actualConfig)

			err = manager.DeleteRealm(realm.Name)
			assert.NoError(t, err)
		})
	}
}

func TestUpdateUserFederationServiceConfigSuccessfully(t *testing.T) {
	testCases := []struct {
		name                  string
		realmNameTemplate     string
		federationServiceName string
	}{
		{name: "realm_with_one_fed_service", realmNameTemplate: "app_with_fed_test_{0}", federationServiceName: "test_ldap"},
	}

	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			manager := createTestRedisDataManager(t)
			realm := data.Realm{
				Name:                   sf.Format(tCase.realmNameTemplate, uuid.New().String()),
				TokenExpiration:        3600,
				RefreshTokenExpiration: 1800,
			}

			err := manager.CreateRealm(realm)
			assert.NoError(t, err)
			r, err := manager.GetRealm(realm.Name)
			assert.NoError(t, err)
			checkRealm(t, &realm, r)

			// Creation of sample UserFederationService
			userFederationServiceConfig := data.UserFederationServiceConfig{
				Name:        tCase.federationServiceName,
				Url:         "ldap://testldap.wissance.com:389",
				Type:        data.LDAP,
				SysUser:     "admin",
				SysPassword: "admin",
			}

			err = manager.CreateUserFederationConfig(realm.Name, userFederationServiceConfig)
			assert.NoError(t, err)

			actualConfig, err := manager.GetUserFederationConfig(realm.Name, userFederationServiceConfig.Name)
			assert.NoError(t, err)
			checkUserFederationConfig(t, &userFederationServiceConfig, actualConfig)

			userFederationServiceConfig.Url = "ldap://domain2.wissance.com:389"
			err = manager.UpdateUserFederationConfig(realm.Name, userFederationServiceConfig.Name, userFederationServiceConfig)
			assert.NoError(t, err)
			actualConfig, err = manager.GetUserFederationConfig(realm.Name, userFederationServiceConfig.Name)
			assert.NoError(t, err)
			checkUserFederationConfig(t, &userFederationServiceConfig, actualConfig)

			err = manager.DeleteRealm(realm.Name)
			assert.NoError(t, err)
		})
	}
}

func TestDeleteUserFederationServiceConfigSuccessfully(t *testing.T) {
	testCases := []struct {
		name                  string
		realmNameTemplate     string
		federationServiceName string
	}{
		{name: "realm_with_one_fed_service", realmNameTemplate: "app_with_fed_test_{0}", federationServiceName: "test_ldap"},
	}

	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			manager := createTestRedisDataManager(t)
			realm := data.Realm{
				Name:                   sf.Format(tCase.realmNameTemplate, uuid.New().String()),
				TokenExpiration:        3600,
				RefreshTokenExpiration: 1800,
			}

			err := manager.CreateRealm(realm)
			assert.NoError(t, err)
			r, err := manager.GetRealm(realm.Name)
			assert.NoError(t, err)
			checkRealm(t, &realm, r)

			// Creation of sample UserFederationService
			userFederationServiceConfig := data.UserFederationServiceConfig{
				Name:        tCase.federationServiceName,
				Url:         "ldap://testldap.wissance.com:389",
				Type:        data.LDAP,
				SysUser:     "admin",
				SysPassword: "admin",
			}

			userFederationServiceConfig2 := data.UserFederationServiceConfig{
				Name:        "another_federation_service",
				Url:         "ldap://testldap2.wissance.com:389",
				Type:        data.LDAP,
				SysUser:     "admin2",
				SysPassword: "admin2",
			}

			err = manager.CreateUserFederationConfig(realm.Name, userFederationServiceConfig)
			assert.NoError(t, err)
			err = manager.CreateUserFederationConfig(realm.Name, userFederationServiceConfig2)
			assert.NoError(t, err)

			err = manager.DeleteUserFederationConfig(realm.Name, userFederationServiceConfig.Name)
			assert.NoError(t, err)
			actualConfig, err := manager.GetUserFederationConfig(realm.Name, userFederationServiceConfig.Name)
			assert.Error(t, err)
			assert.Nil(t, actualConfig)

			actualConfig, err = manager.GetUserFederationConfig(realm.Name, userFederationServiceConfig2.Name)
			assert.NoError(t, err)
			checkUserFederationConfig(t, &userFederationServiceConfig2, actualConfig)

			err = manager.DeleteRealm(realm.Name)
			assert.NoError(t, err)
		})
	}
}

func createTestRedisDataManager(t *testing.T) *RedisDataManager {
	rndNamespace := sf.Format("ferrum_test_{0}", uuid.New().String())
	dataSourceCfg := config.DataSourceConfig{
		Type:   config.REDIS,
		Source: testRedisSource,
		Options: map[config.DataSourceConnOption]string{
			config.Namespace: rndNamespace,
			config.DbNumber:  "0",
		},
		Credentials: &config.CredentialsConfig{
			Username: testUser,
			Password: testUserPassword,
		},
	}

	loggerCfg := config.LoggingConfig{}

	logger := logging.CreateLogger(&loggerCfg)
	manager, err := CreateRedisDataManager(&dataSourceCfg, logger)
	require.NoError(t, err)
	return manager
}

func checkRealm(t *testing.T, expected *data.Realm, actual *data.Realm) {
	assert.Equal(t, expected.Name, actual.Name)
	assert.Equal(t, expected.TokenExpiration, actual.TokenExpiration)
	assert.Equal(t, expected.RefreshTokenExpiration, actual.RefreshTokenExpiration)
	checkUserFederationConfigs(t, &expected.UserFederationServices, &actual.UserFederationServices)
}

func checkClients(t *testing.T, expected *[]data.Client, actual *[]data.Client) {
	assert.Equal(t, len(*expected), len(*actual))
	for _, e := range *expected {
		found := false
		for _, a := range *actual {
			if e.Name == a.Name {
				checkClient(t, &e, &a)
				found = true
				break
			}
		}
		assert.True(t, found)
	}
}

func checkClient(t *testing.T, expected *data.Client, actual *data.Client) {
	assert.Equal(t, expected.Name, actual.Name)
	assert.Equal(t, expected.Type, actual.Type)
	assert.Equal(t, expected.ID, actual.ID)
	assert.Equal(t, expected.Auth.Type, actual.Auth.Type)
	assert.Equal(t, expected.Auth.Value, actual.Auth.Value)
}

func checkUsers(t *testing.T, expected *[]data.User, actual *[]data.User) {
	assert.Equal(t, len(*expected), len(*actual))
	for _, e := range *expected {
		// check and find actual ....
		found := false
		for _, a := range *actual {
			if e.GetId() == a.GetId() {
				checkUser(t, &e, &a)
				found = true
				break
			}
		}
		assert.True(t, found)
	}
}

func checkUser(t *testing.T, expected *data.User, actual *data.User) {
	assert.Equal(t, (*expected).GetId(), (*actual).GetId())
	assert.Equal(t, (*expected).GetUsername(), (*actual).GetUsername())
	assert.Equal(t, (*expected).GetPasswordHash(), (*actual).GetPasswordHash())
}

func checkUserFederationConfigs(t *testing.T, expected *[]data.UserFederationServiceConfig, actual *[]data.UserFederationServiceConfig) {
	assert.Equal(t, len(*expected), len(*actual))
	for _, e := range *expected {
		// check and find actual ....
		found := false
		for _, a := range *actual {
			if e.Name == a.Name {
				checkUserFederationConfig(t, &e, &a)
				found = true
				break
			}
		}
		assert.True(t, found)
	}
}

func checkUserFederationConfig(t *testing.T, expected *data.UserFederationServiceConfig, actual *data.UserFederationServiceConfig) {
	assert.Equal(t, expected.Name, actual.Name)
	assert.Equal(t, expected.Type, actual.Type)
	assert.Equal(t, expected.Url, actual.Url)
	assert.Equal(t, expected.EntryPoint, actual.EntryPoint)
	assert.Equal(t, expected.SysUser, actual.SysUser)
	assert.Equal(t, expected.SysPassword, actual.SysPassword)
}
