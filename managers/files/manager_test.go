package files

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wissance/Ferrum/config"
	"github.com/wissance/Ferrum/data"
	"github.com/wissance/Ferrum/logging"
)

const testDataFile = "test_data.json"

func TestGetRealmSuccessfully(t *testing.T) {
	manager := createTestFileDataManager(t)
	expectedRealm := data.Realm{
		Name:                   "myapp",
		TokenExpiration:        330,
		RefreshTokenExpiration: 200,
	}
	r, err := manager.GetRealm("myapp")
	assert.NoError(t, err)
	checkRealm(t, &expectedRealm, r)
}

func TestGetClientSuccessfully(t *testing.T) {
	manager := createTestFileDataManager(t)
	realm := "myapp"
	clientId, _ := uuid.Parse("d4dc483d-7d0d-4d2e-a0a0-2d34b55e5a14")
	expectedClient := data.Client{
		ID:   clientId,
		Name: "test-service-app-client",
		Type: data.Confidential,
		Auth: data.Authentication{
			Type:  data.ClientIdAndSecrets,
			Value: "fb6Z4RsOadVycQoeQiN57xpu8w8wplYz",
		},
	}

	c, err := manager.GetClient(realm, expectedClient.Name)
	assert.NoError(t, err)
	checkClient(t, &expectedClient, c)
}

func TestGetUserSuccessfully(t *testing.T) {
	manager := createTestFileDataManager(t)
	realm := "myapp"
	userName := "admin"

	userJson := `{"info": {
                        "sub": "667ff6a7-3f6b-449b-a217-6fc5d9ac0723",
                        "email_verified": false,
                        "roles": [
                            "admin"
                        ],
                        "name": "admin sys",
                        "preferred_username": "admin",
                        "given_name": "admin",
                        "family_name": "sys"
                    },
                    "credentials": {
                        "password": "1s2d3f4g90xs"
                    }}`

	var rawUser interface{}
	err := json.Unmarshal([]byte(userJson), &rawUser)
	assert.NoError(t, err)
	expectedUser := data.CreateUser(rawUser, nil)
	user, err := manager.GetUser(realm, userName)
	assert.NoError(t, err)
	checkUser(t, &expectedUser, &user)
}

func TestGetUserByIdSuccessfully(t *testing.T) {
	manager := createTestFileDataManager(t)
	realm := "myapp"
	userId, _ := uuid.Parse("667ff6a7-3f6b-449b-a217-6fc5d9ac0723")

	userJson := `{"info": {
                        "sub": "667ff6a7-3f6b-449b-a217-6fc5d9ac0723",
                        "email_verified": false,
                        "roles": [
                            "admin"
                        ],
                        "name": "admin sys",
                        "preferred_username": "admin",
                        "given_name": "admin",
                        "family_name": "sys"
                    },
                    "credentials": {
                        "password": "1s2d3f4g90xs"
                    }}`

	var rawUser interface{}
	err := json.Unmarshal([]byte(userJson), &rawUser)
	assert.NoError(t, err)
	expectedUser := data.CreateUser(rawUser, nil)
	user, err := manager.GetUserById(realm, userId)
	assert.NoError(t, err)
	checkUser(t, &expectedUser, &user)
}

func createTestFileDataManager(t *testing.T) *FileDataManager {
	loggerCfg := config.LoggingConfig{}

	logger := logging.CreateLogger(&loggerCfg)

	manager, err := CreateFileDataManager(testDataFile, logger)
	require.NoError(t, err)
	return manager
}

func checkRealm(t *testing.T, expected *data.Realm, actual *data.Realm) {
	assert.Equal(t, expected.Name, actual.Name)
	assert.Equal(t, expected.TokenExpiration, actual.TokenExpiration)
	assert.Equal(t, expected.RefreshTokenExpiration, actual.RefreshTokenExpiration)
}

// nolint unused
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

// nolint unused
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
