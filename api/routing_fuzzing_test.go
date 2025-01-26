package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/wissance/Ferrum/application"
	"github.com/wissance/Ferrum/config"
	"github.com/wissance/Ferrum/data"
	"github.com/wissance/Ferrum/dto"
	"github.com/wissance/Ferrum/utils/encoding"
	sf "github.com/wissance/stringFormatter"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAccessTokenExpiration  = 10
	testRefreshTokenExpiration = 5
	testRealm1                 = "testrealm1"
	testClient1                = "testclient1"
	testClient1Secret          = "fb6Z4RsOadVycQoeQiN57xpu8w8wplYz"
)

var (
	testSalt           = "salt"
	encoder            = encoding.NewPasswordJsonEncoder(testSalt)
	testHashedPassowrd = encoder.GetB64PasswordHash("1234567890")
	testKey            = []byte("qwerty1234567890")
	testServerData     = data.ServerData{
		Realms: []data.Realm{
			{
				Name: testRealm1, TokenExpiration: testAccessTokenExpiration, RefreshTokenExpiration: testRefreshTokenExpiration,
				Clients: []data.Client{
					{Name: testClient1, Type: data.Confidential, Auth: data.Authentication{
						Type:  data.ClientIdAndSecrets,
						Value: testClient1Secret,
					}},
				},
				Users: []interface{}{
					map[string]interface{}{
						"info": map[string]interface{}{
							"sub":  "667ff6a7-3f6b-449b-a217-6fc5d9ac0723",
							"name": "vano", "preferred_username": "vano",
							"given_name": "vano ivanov", "family_name": "ivanov", "email_verified": true,
						},
						"credentials": map[string]interface{}{"password": testHashedPassowrd},
					},
				},
				PasswordSalt: testSalt,
			},
		},
	}
)

var (
	loggingConfig = config.LoggingConfig{Level: "info", Appenders: []config.AppenderConfig{{Level: "info", Type: config.Console}}}
	httpAppConfig = config.AppConfig{
		ServerCfg: config.ServerConfig{Schema: config.HTTP, Address: "127.0.0.1", Port: 8284},
		Logging:   loggingConfig, DataSource: config.DataSourceConfig{Type: config.FILE},
	}
)

func FuzzTestIssueNewTokenWithWrongClientId(f *testing.F) {
	f.Add("\x00testclient1")
	f.Add("\x00test_client_1")
	f.Add("")
	f.Add("0")
	f.Add("00")

	f.Fuzz(func(t *testing.T, clientId string) {
		initApp(t)
		issueNewToken(t, clientId, testClient1Secret, "vano", "1234567890", 400)
	})
}

func FuzzTestIssueNewTokenWithWrongClientSecret(f *testing.F) {
	f.Add("\x00fb6Z4RsOadVycQoeQiN57xpu8w8wplYz")
	f.Add("fb6Z4RsOadVycQoeQiN57xpu8w8wplYz_!")
	f.Add("")

	f.Fuzz(func(t *testing.T, clientSecret string) {
		initApp(t)
		issueNewToken(t, testClient1, clientSecret, "vano", "1234567890", 400)
	})
}

func FuzzTestIssueNewTokenWithWrongUsername(f *testing.F) {
	f.Add("\x00vano")
	f.Add("!")
	f.Add("")

	f.Fuzz(func(t *testing.T, username string) {
		initApp(t)
		issueNewToken(t, testClient1, testClient1Secret, username, "1234567890", 401)
	})
}

func FuzzTestIssueNewTokenWithWrongPassword(f *testing.F) {
	f.Add("\x001234567890")
	f.Add("!")
	f.Add("")

	f.Fuzz(func(t *testing.T, password string) {
		initApp(t)
		issueNewToken(t, testClient1, testClient1Secret, "vano", password, 401)
	})
}

func FuzzTestIntrospectTokenWithWrongClientId(f *testing.F) {
	f.Add("\x001234567890")
	f.Add("!")
	f.Add("")

	f.Fuzz(func(t *testing.T, clientId string) {
		initApp(t)
		token := getToken(t)
		checkIntrospectToken(t, token.AccessToken, clientId, testClient1Secret, testRealm1, 401)
	})
}

func FuzzTestIntrospectTokenWithWrongSecret(f *testing.F) {
	f.Add("\x001234567890")
	f.Add("!")
	f.Add("")

	f.Fuzz(func(t *testing.T, clientSecret string) {
		initApp(t)
		token := getToken(t)
		checkIntrospectToken(t, token.AccessToken, testClient1, clientSecret, testRealm1, 401)
	})
}

func FuzzTestIntrospectTokenWithWrongToken(f *testing.F) {
	f.Add(" ")
	f.Add("\x001234567890")
	f.Add("!")
	f.Add("")

	f.Fuzz(func(t *testing.T, token string) {
		initApp(t)
		checkIntrospectToken(t, token, testClient1, testClient1Secret, testRealm1, 401)
	})
}

func FuzzTestRefreshTokenWithWrongToken(f *testing.F) {
	f.Add("\x00testclient1")
	f.Add("\x00test_client_1")
	f.Add("")
	f.Add("0")
	f.Add("00")

	f.Fuzz(func(t *testing.T, token string) {
		initApp(t)
		refreshToken(t, testClient1, testClient1Secret, token, 401)
	})
}

func FuzzTestGetUserInfoWithWrongToken(f *testing.F) {
	f.Add("\t")
	f.Add("00")
	f.Add("  ")
	f.Add("\n\n")

	f.Fuzz(func(t *testing.T, token string) {
		initApp(t)
		expectedStatusCode := 401
		if !isTokenValid(t, token) || len(token) == 0 {
			expectedStatusCode = 400
		}
		userInfoUrlTemplate := "{0}/auth/realms/{1}/protocol/openid-connect/userinfo/"
		doRequest(
			t, "GET", userInfoUrlTemplate, testRealm1, nil,
			expectedStatusCode, map[string]string{"Authorization": "Bearer " + token},
		)
	})
}

func initApp(t *testing.T) application.AppRunner {
	t.Helper()
	app := application.CreateAppWithData(&httpAppConfig, &testServerData, testKey, true)
	t.Cleanup(func() {
		_, err := app.Stop(context.Background())
		require.NoError(t, err)
	})
	res, err := app.Init()
	assert.True(t, res)
	assert.Nil(t, err)
	res, err = app.Start()
	assert.True(t, res)
	assert.Nil(t, err)
	return app
}

func issueNewToken(t *testing.T, clientId, clientSecret, username, password string, expectedStatus int) *http.Response {
	t.Helper()
	urlTemplate := "{0}/auth/realms/{1}/protocol/openid-connect/token"
	issueNewTokenUrl := makeUrl(t, urlTemplate, testRealm1)
	getTokenData := setGetTokenFormData(clientId, clientSecret, "password", username, password, "")
	return doPostForm(t, issueNewTokenUrl, getTokenData, expectedStatus)
}

func refreshToken(t *testing.T, clientId, clientSecret, refreshToken string, expectedStatus int) *http.Response {
	t.Helper()
	urlTemplate := "{0}/realms/{1}/protocol/openid-connect/token"
	refreshTokenUrl := makeUrl(t, urlTemplate, testRealm1)
	getTokenData := setGetTokenFormData(clientId, clientSecret, "refresh_token", "", "", refreshToken)
	return doPostForm(t, refreshTokenUrl, getTokenData, expectedStatus)
}

func setGetTokenFormData(clientId, clientSecret, grantType, username, password, refreshToken string) url.Values {
	getTokenData := url.Values{}
	getTokenData.Set("client_id", clientId)
	getTokenData.Set("client_secret", clientSecret)
	getTokenData.Set("scope", "profile")
	getTokenData.Set("grant_type", grantType)
	getTokenData.Set("username", username)
	getTokenData.Set("password", password)
	getTokenData.Set("refresh_token", refreshToken)
	return getTokenData
}

func doPostForm(t *testing.T, reqUrl string, urlData url.Values, expectedStatus int) *http.Response {
	t.Helper()
	response, err := http.PostForm(reqUrl, urlData)
	require.NoError(t, err)
	if response != nil {
		require.Equal(t, response.StatusCode, expectedStatus)
	}
	return response
}

func doRequest(t *testing.T, method, urlTemplate, realm string,
	formData *url.Values, expectedStatus int, headers map[string]string,
) {
	var err error
	var request *http.Request
	reqUrl := makeUrl(t, urlTemplate, realm)
	if formData != nil {
		request, err = http.NewRequest(method, reqUrl, strings.NewReader(formData.Encode()))
	} else {
		request, err = http.NewRequest(method, reqUrl, nil)
	}

	client := http.Client{}
	assert.NoError(t, err)

	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, _ := client.Do(request)
	if response != nil {
		assert.Equal(t, expectedStatus, response.StatusCode)
	}
}

func getToken(t *testing.T) dto.Token {
	t.Helper()
	response := issueNewToken(t, testClient1, testClient1Secret, "vano", "1234567890", 200)
	token := getDataFromResponse[dto.Token](t, response)
	return token
}

func checkIntrospectToken(
	t *testing.T, token, clientId, clientSecret, realm string, expectedStatus int,
) {
	t.Helper()
	urlTemplate := sf.Format("{0}/auth/realms/{1}/protocol/openid-connect/token/introspect")
	formData := url.Values{}
	formData.Set("token_type_hint", "requesting_party_token")
	formData.Set("token", token)
	httpBasicAuth := base64.StdEncoding.EncodeToString([]byte(clientId + ":" + clientSecret))

	headers := map[string]string{
		"Authorization": "Basic " + httpBasicAuth,
		"Content-Type":  "application/x-www-form-urlencoded",
	}

	doRequest(t, "POST", urlTemplate, realm, &formData, expectedStatus, headers)
}

func makeUrl(t *testing.T, urlTemplate, realm string) string {
	t.Helper()
	serverAddress := sf.Format("{0}:{1}", httpAppConfig.ServerCfg.Address, httpAppConfig.ServerCfg.Port)
	baseUrl := sf.Format("{0}://{1}", httpAppConfig.ServerCfg.Schema, serverAddress)
	url_ := sf.Format(urlTemplate, baseUrl, realm)
	return url_
}

func getDataFromResponse[TR dto.Token | dto.ErrorDetails](t *testing.T, response *http.Response) TR {
	t.Helper()
	responseBody, err := io.ReadAll(response.Body)
	assert.Nil(t, err)
	var result TR
	err = json.Unmarshal(responseBody, &result)
	assert.Nil(t, err)
	return result
}

func isTokenValid(t *testing.T, token string) bool {
	// Checking that the token doesn't contains space characters only.
	// If yes, then the token is not valid - the expected status code is 400. Otherwise - 401.
	t.Helper()
	pattern := "[ \n\t]+"
	match, _ := regexp.MatchString(pattern, token)
	return !match
}
