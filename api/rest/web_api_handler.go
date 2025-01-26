package rest

import (
	"encoding/base64"
	e "errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"github.com/wissance/Ferrum/data"
	"github.com/wissance/Ferrum/dto"
	"github.com/wissance/Ferrum/errors"
	"github.com/wissance/Ferrum/globals"
	sf "github.com/wissance/stringFormatter"
)

// IssueNewToken this function is a Http Request Handler that is responsible for issue New or Refresh existing tokens
// @Summary Issues new or Refreshes existing token
// @Description Issues new or Refreshes existing token
// @Tags token
// @Accept x-www-form-urlencoded
// @Produce json
// @Param function body dto.TokenGenerationData true "Token generation data"
// @Param realm path string true "Realm"
// @Success 200 {object} dto.Token
// @Failure 400 {string} dto.ErrorDetails
// @Failure 401 {string} dto.ErrorDetails
// @Failure 404 {string} dto.ErrorDetails
// @Router /auth/realms/{realm}/protocol/openid-connect/token [post]
// @Router /realms/{realm}/protocol/openid-connect/token [post]
func (wCtx *WebApiContext) IssueNewToken(respWriter http.ResponseWriter, request *http.Request) {
	/* For issue new token user should send POST request of type x-www-from-urlencoded with following pairs key=value
	 * grant_type=password (password only supported), client_id (data.Client name), if client is Confidential also client_secret,
	 * scope=profile email, username and password
	 * For refreshing existing token user should send POST request of type x-www-from-urlencoded with following
	 * pairs key=value client_id, client_secret (if data.Client is Confidential), grant_type=refresh_token and refresh_token itself
	 */
	beforeHandle(&respWriter)
	var result interface{}
	status := http.StatusOK

	vars := mux.Vars(request)
	realm := vars[globals.RealmPathVar]
	if !Validate(realm) {
		wCtx.Logger.Debug(sf.Format("New token issue: is invalid realmName: '{0}'", realm))
		status = http.StatusBadRequest
		result = dto.ErrorDetails{Msg: sf.Format(errors.InvalidRealm, realm)}
		afterHandle(&respWriter, status, &result)
		return
	}
	realmPtr, realmReadErr := (*wCtx.DataProvider).GetRealm(realm)
	if realmReadErr != nil {
		if e.As(realmReadErr, &errors.ErrDataSourceNotAvailable) {
			status = http.StatusServiceUnavailable
			wCtx.Logger.Error("Data provider not available")
			result = dto.ErrorDetails{Msg: errors.ServiceIsUnavailable}
		} else {
			if e.As(realmReadErr, &errors.EmptyNotFoundErr) {
				status = http.StatusNotFound
				wCtx.Logger.Debug("New token issue: realm doesn't exist")
				result = dto.ErrorDetails{Msg: sf.Format(errors.RealmDoesNotExistsTemplate, realm)}
			} else {
				status = http.StatusInternalServerError
				wCtx.Logger.Error(sf.Format("Other error occurred: {0}", realmReadErr.Error()))
				result = dto.ErrorDetails{Msg: sf.Format(errors.OtherAppError, realm)}
			}
		}
	} else {
		tokenGenerationData := dto.TokenGenerationData{}
		err := request.ParseForm()
		if err != nil {
			status = http.StatusBadRequest
			wCtx.Logger.Debug("New token issue: body is bad (unable to unmarshal to dto.TokenGenerationData)")
			result = dto.ErrorDetails{Msg: errors.BadBodyForTokenGenerationMsg}
		} else {
			decoder := schema.NewDecoder()
			err = decoder.Decode(&tokenGenerationData, request.PostForm)
			if err != nil {
				// todo (UMV): log events
				status = http.StatusBadRequest
				wCtx.Logger.Debug("New token issue: body is bad (unable to unmarshal to dto.TokenGenerationData)")
				result = dto.ErrorDetails{Msg: errors.BadBodyForTokenGenerationMsg}
			} else {
				var currentUser data.User
				var userId uuid.UUID
				issueTokens := false
				// 0. Check whether we deal with issuing a new token or refresh previous one
				isRefresh := isTokenRefreshRequest(&tokenGenerationData)
				if isRefresh {
					// 1-2. Validate refresh token and check is it fresh enough
					session := (*wCtx.Security).GetSessionByRefreshToken(realm, &tokenGenerationData.RefreshToken)
					if session == nil {
						status = http.StatusUnauthorized
						result = dto.ErrorDetails{Msg: errors.InvalidTokenMsg, Description: errors.TokenIsNotActive}
					} else {
						userId = session.UserId
						sessionExpired, refreshExpired := (*wCtx.Security).CheckSessionAndRefreshExpired(realm, userId)
						if sessionExpired {
							// session expired, should request new one
							status = http.StatusBadRequest
							result = dto.ErrorDetails{Msg: errors.InvalidTokenMsg, Description: errors.TokenIsNotActive}
						} else {
							if refreshExpired {
								status = http.StatusBadRequest
								result = dto.ErrorDetails{Msg: errors.InvalidTokenMsg, Description: errors.TokenIsNotActive}
							} else {
								currentUser = (*wCtx.Security).GetCurrentUserById(realmPtr.Name, userId)
								if currentUser != nil {
									issueTokens = true
								} else {
									result = dto.ErrorDetails{Msg: errors.InvalidTokenMsg, Description: errors.TokenIsNotActive}
								}
							}
						}

					}

				} else {
					check := (*wCtx.Security).Validate(&tokenGenerationData, realmPtr)
					// 1. Pair client_id && client_secret validation
					if check != nil {
						status = http.StatusBadRequest
						wCtx.Logger.Debug("New token issue: client data is invalid (client_id or client_secret)")
						result = dto.ErrorDetails{Msg: check.Msg, Description: check.Description}
					} else {
						// 2. User credentials validation
						check = (*wCtx.Security).CheckCredentials(&tokenGenerationData, realmPtr.Name)
						if check != nil {
							wCtx.Logger.Debug("New token issue: invalid user credentials (username or password)")
							status = http.StatusUnauthorized
							result = dto.ErrorDetails{Msg: check.Msg, Description: check.Description}
						} else {
							currentUser = (*wCtx.Security).GetCurrentUserByName(realmPtr.Name, tokenGenerationData.Username)
							userId = currentUser.GetId()
							issueTokens = true
						}
					}
				}
				if issueTokens {
					// 3. Create access token && refresh token
					duration := realmPtr.TokenExpiration
					refresh := realmPtr.RefreshTokenExpiration
					refreshDuration := realmPtr.RefreshTokenExpiration
					// 4. Save session
					sessionId := (*wCtx.Security).StartOrUpdateSession(realm, userId, duration, refresh)
					session := (*wCtx.Security).GetSession(realm, userId)
					// 5. Generate new tokens
					accessToken := wCtx.TokenGenerator.GenerateJwtAccessToken(wCtx.getRealmBaseUrl(realm), string(BearerToken),
						globals.ProfileEmailScope, session, currentUser)
					refreshToken := wCtx.TokenGenerator.GenerateJwtRefreshToken(wCtx.getRealmBaseUrl(realm), string(RefreshToken),
						globals.ProfileEmailScope, session)
					(*wCtx.Security).AssignTokens(realm, userId, &accessToken, &refreshToken)
					// 6. Assign token to result
					result = dto.Token{
						AccessToken: accessToken, Expires: duration, RefreshToken: refreshToken,
						RefreshExpires: refreshDuration, TokenType: string(BearerToken), NotBeforePolicy: 0, Session: sessionId.String(),
					}

				}
			}
		}
	}

	afterHandle(&respWriter, status, &result)
}

// GetUserInfo this function is a Http Request Handler that is responsible for getting public data.UserInfo
// @Summary Getting UserInfo by token
// @Description Getting UserInfo by token
// @Tags users
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer TOKEN"
// @Param realm path string true "Realm"
// @Success 200 {object} interface{}
// @Failure 400 {string} dto.ErrorDetails
// @Failure 401 {string} dto.ErrorDetails
// @Failure 404 {string} dto.ErrorDetails
// @Router /auth/realms/{realm}/protocol/openid-connect/userinfo [get]
// @Router /realms/{realm}/protocol/openid-connect/userinfo [get]
func (wCtx *WebApiContext) GetUserInfo(respWriter http.ResponseWriter, request *http.Request) {
	/* This function return public data.User , user must provide Authorization HTTP Header with value Bearer {access_token}
	 */
	beforeHandle(&respWriter)
	var result interface{}
	status := http.StatusOK

	vars := mux.Vars(request)
	realm := vars[globals.RealmPathVar]
	if !Validate(realm) {
		wCtx.Logger.Debug(sf.Format("Get UserInfo: is invalid realmName: '{0}'", realm))
		status = http.StatusBadRequest
		result := dto.ErrorDetails{Msg: sf.Format(errors.InvalidRealm, realm)}
		afterHandle(&respWriter, status, &result)
		return
	}
	realmPtr, realmReadErr := (*wCtx.DataProvider).GetRealm(realm)
	if realmReadErr != nil {
		if e.As(realmReadErr, &errors.ErrDataSourceNotAvailable) {
			status = http.StatusServiceUnavailable
			wCtx.Logger.Error("Data provider not available")
			result = dto.ErrorDetails{Msg: errors.ServiceIsUnavailable}
		} else {
			if e.As(realmReadErr, &errors.EmptyNotFoundErr) {
				status = http.StatusNotFound
				wCtx.Logger.Debug("Get UserInfo: realm doesn't exist")
				result = dto.ErrorDetails{Msg: sf.Format(errors.RealmDoesNotExistsTemplate, realm)}
			} else {
				status = http.StatusInternalServerError
				wCtx.Logger.Error(sf.Format("Other error occurred: {0}", realmReadErr.Error()))
				result = dto.ErrorDetails{Msg: sf.Format(errors.OtherAppError, realm)}
			}
		}
	} else {
		// Just get access token,  find user + session
		authorization := request.Header.Get(authorizationHeader)
		parts := strings.Split(authorization, " ")
		if parts[0] != string(BearerToken) {
			wCtx.Logger.Debug("Get userinfo: expected only Bearer authorization yet")
			status = http.StatusBadRequest
			result = dto.ErrorDetails{Msg: errors.InvalidRequestMsg, Description: errors.InvalidRequestDesc}
		} else if len(parts) < 2 {
			wCtx.Logger.Debug("Get userinfo: token not provided")
			status = http.StatusBadRequest
			result = dto.ErrorDetails{Msg: errors.InvalidRequestMsg, Description: errors.InvalidRequestDesc}

		} else {
			session := (*wCtx.Security).GetSessionByAccessToken(realm, &parts[1])
			if session == nil {
				wCtx.Logger.Debug("Get userinfo: invalid token")
				status = http.StatusUnauthorized
				result = dto.ErrorDetails{Msg: errors.InvalidTokenMsg, Description: errors.InvalidTokenDesc}
			} else {
				if session.Expired.Before(time.Now()) {
					status = http.StatusUnauthorized
					wCtx.Logger.Debug("Get userinfo: token expired")
					result = dto.ErrorDetails{Msg: errors.InvalidTokenMsg, Description: errors.InvalidTokenDesc}
				} else {
					user, _ := (*wCtx.DataProvider).GetUserById(realmPtr.Name, session.UserId)
					if user != nil {
						result = user.GetUserInfo()
					}
				}
			}
		}
	}

	afterHandle(&respWriter, status, &result)
}

// Introspect - is a function that analyzes state of a token and getting some data from it
// @Summary Analyzes state of a token and getting some data from it
// @Description Analyzes state of a token and getting some data from it
// @Tags token
// @Accept json
// @Produce json
// @Param Authorization header string true "Basic client_id:client_secret as Base64 i.e. Basic V2lzc2FuY2VXZWJEZW1vOmZiNlo0UnNPYWRWeWNRb2VRaU41N3hwdTh3OHcxMTEx"
// @Param realm path string true "Realm"
// @Success 200 {object} dto.IntrospectTokenResult
// @Failure 400 {string} dto.ErrorDetails
// @Failure 401 {string} dto.ErrorDetails
// @Failure 404 {string} dto.ErrorDetails
// @Router /auth/realms/{realm}/protocol/openid-connect/token/introspect [post]
// @Router /realms/{realm}/protocol/openid-connect/token/introspect [post]
func (wCtx *WebApiContext) Introspect(respWriter http.ResponseWriter, request *http.Request) {
	/* To call introspect we should form a POST HTTP Request with Authorization header, value for this header is: Basic base64({client_id}:{client_secret})
	 * Consider we have client_id -> test-service-app-client and client_secret -> fb6Z4RsOadVycQoeQiN57xpu8w8wplYz, we get following base64 value for this pair:
	 * dGVzdC1zZXJ2aWNlLWFwcC1jbGllbnQ6ZmI2WjRSc09hZFZ5Y1FvZVFpTjU3eHB1OHc4d3BsWXo= (you could use -https://www.base64encode.org/)
	 * In body of this request we should pass token as key, and value as x-www-urlencoded.
	 */
	beforeHandle(&respWriter)
	vars := mux.Vars(request)
	realm := vars[globals.RealmPathVar]
	if !Validate(realm) {
		wCtx.Logger.Debug(sf.Format("Introspect: is invalid realmName: '{0}'", realm))
		status := http.StatusBadRequest
		result := dto.ErrorDetails{Msg: sf.Format(errors.InvalidRealm, realm)}
		afterHandle(&respWriter, status, &result)
		return
	}
	realmPtr, realmReadErr := (*wCtx.DataProvider).GetRealm(realm)
	if realmReadErr != nil {
		var status int
		var result interface{}
		if e.As(realmReadErr, &errors.ErrDataSourceNotAvailable) {
			status = http.StatusServiceUnavailable
			wCtx.Logger.Error("Data provider not available")
			result = dto.ErrorDetails{Msg: errors.ServiceIsUnavailable}
		} else {
			if e.As(realmReadErr, &errors.EmptyNotFoundErr) {
				status = http.StatusNotFound
				wCtx.Logger.Debug("Introspect: realm doesn't exist")
				result = dto.ErrorDetails{Msg: sf.Format(errors.RealmDoesNotExistsTemplate, realm)}
			} else {
				status = http.StatusInternalServerError
				wCtx.Logger.Error(sf.Format("Other error occurred: {0}", realmReadErr.Error()))
				result = dto.ErrorDetails{Msg: sf.Format(errors.OtherAppError, realm)}
			}
		}
		afterHandle(&respWriter, status, &result)
		return
	}
	authorization := request.Header.Get(authorizationHeader)
	parts := strings.Split(authorization, " ")
	if parts[0] != "Basic" {
		status := http.StatusBadRequest
		wCtx.Logger.Debug(sf.Format("Introspect: Basic value not provided in Authorization header value - \"{0}\"", parts[0]))
		result := dto.ErrorDetails{Msg: errors.InvalidRequestMsg, Description: errors.InvalidRequestDesc}
		afterHandle(&respWriter, status, &result)
		return
	}
	basicString, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		status := http.StatusBadRequest
		wCtx.Logger.Debug(sf.Format("Introspect: invalid client credentials encoding, should be base64, decoding error: {0}", err.Error()))
		result := dto.ErrorDetails{Msg: errors.InvalidClientMsg, Description: errors.InvalidClientCredentialDesc}
		afterHandle(&respWriter, status, &result)
		return
	}
	secretPair := strings.Split(string(basicString), ":")
	checkResult := (*wCtx.Security).Validate(&dto.TokenGenerationData{
		ClientSecret: secretPair[1],
		ClientId:     secretPair[0],
	}, realmPtr)
	if checkResult != nil {
		status := http.StatusUnauthorized
		wCtx.Logger.Debug("Introspect: invalid client credentials")
		result := dto.ErrorDetails{Msg: errors.InvalidClientMsg, Description: errors.InvalidClientCredentialDesc}
		afterHandle(&respWriter, status, &result)
		return
	}
	token := request.FormValue(globals.TokenFormKey)
	session := (*wCtx.Security).GetSessionByAccessToken(realm, &token)
	if session == nil {
		status := http.StatusUnauthorized
		result := dto.ErrorDetails{Msg: errors.InvalidTokenMsg, Description: errors.InvalidTokenDesc}
		wCtx.Logger.Debug("Introspect: invalid token")
		afterHandle(&respWriter, status, &result)
		return
	}
	active := !session.Expired.Before(time.Now())
	status := http.StatusOK
	authTokenType := string(BearerToken)
	result := dto.IntrospectTokenResult{
		Active: active,
		Type:   authTokenType,
		Exp:    realmPtr.TokenExpiration,
	}
	afterHandle(&respWriter, status, &result)
}

// GetOpenIdConfiguration this function is a Http Request Handler that is responsible for getting available URL and some other configs related to OpenId
// @Summary Getting Info about Url and other config values
// @Description Getting Info about Url and other config values
// @Tags configuration
// @Accept json
// @Produce json
// @Param realm path string true "Realm"
// @Success 200 {object} dto.OpenIdConfiguration
// @Failure 400 {string} dto.ErrorDetails
// @Failure 404 {string} dto.ErrorDetails
// @Router /auth/realms/{realm}/.well-known/openid-configuration [get]
// @Router /realms/{realm}/.well-known/openid-configuration [get]
func (wCtx *WebApiContext) GetOpenIdConfiguration(respWriter http.ResponseWriter, request *http.Request) {
	/* This function return public data.User , user must provide Authorization HTTP Header with value Bearer {access_token}
	 */
	beforeHandle(&respWriter)
	status := http.StatusOK
	var result interface{}

	vars := mux.Vars(request)
	realm := vars[globals.RealmPathVar]
	if !Validate(realm) {
		wCtx.Logger.Debug(sf.Format("Get OpenIdConfig: is invalid realmName: '{0}'", realm))
		status := http.StatusBadRequest
		result := dto.ErrorDetails{Msg: sf.Format(errors.InvalidRealm, realm)}
		afterHandle(&respWriter, status, &result)
		return
	}
	_, realmReadErr := (*wCtx.DataProvider).GetRealm(realm)
	if realmReadErr != nil {
		if e.As(realmReadErr, &errors.ErrDataSourceNotAvailable) {
			status = http.StatusServiceUnavailable
			wCtx.Logger.Error("Data provider not available")
			result = dto.ErrorDetails{Msg: errors.ServiceIsUnavailable}
		} else {
			if e.As(realmReadErr, &errors.EmptyNotFoundErr) {
				status = http.StatusNotFound
				wCtx.Logger.Debug("Get OpenIdConfig: realm doesn't exist")
				result = dto.ErrorDetails{Msg: sf.Format(errors.RealmDoesNotExistsTemplate, realm)}
			} else {
				status = http.StatusInternalServerError
				wCtx.Logger.Error(sf.Format("Other error occurred: {0}", realmReadErr.Error()))
				result = dto.ErrorDetails{Msg: sf.Format(errors.OtherAppError, realm)}
			}
		}
	} else {
		realmPath := sf.Format("auth/realms/{0}", realm)
		protocolPath := "protocol/openid-connect"
		// What is important is that server could be behind reverse proxy
		fullAddress := sf.Format("{0}://{1}", wCtx.Schema, wCtx.Address)
		openIdConfig := dto.OpenIdConfiguration{}
		openIdConfig.Issuer = sf.Format("{0}/{1}", fullAddress, realmPath)
		openIdConfig.TokenEndpoint = sf.Format("{0}/{1}/token", openIdConfig.Issuer, protocolPath)
		openIdConfig.IntrospectionEndpoint = sf.Format("{0}/{1}/introspect", openIdConfig.Issuer, protocolPath)
		openIdConfig.UserInfoEndpoint = sf.Format("{0}/{1}/userinfo", openIdConfig.Issuer, protocolPath)
		openIdConfig.AuthorizationEndpoint = sf.Format("{0}/{1}/auth", openIdConfig.Issuer, protocolPath)
		// TODO(UMV): assign other endpoint as soon
		openIdConfig.ClaimsSupported = wCtx.AuthDefs.SupportedClaims
		openIdConfig.ClaimTypesSupported = wCtx.AuthDefs.SupportedClaimTypes
		openIdConfig.GrantTypesSupported = wCtx.AuthDefs.SupportedGrantTypes
		openIdConfig.CodeChallengeMethodsSupported = []string{}
		openIdConfig.ResponseModesSupported = wCtx.AuthDefs.SupportedResponses
		openIdConfig.ResponseTypesSupported = wCtx.AuthDefs.SupportedResponseTypes
		result = openIdConfig
	}

	afterHandle(&respWriter, status, &result)
}

func (wCtx *WebApiContext) getRealmBaseUrl(realm string) string {
	return sf.Format("/{0}/{1}/auth/realms/{2}", wCtx.Schema, wCtx.Address, realm)
}

func isTokenRefreshRequest(tokenIssueData *dto.TokenGenerationData) bool {
	if len(tokenIssueData.RefreshToken) == 0 || tokenIssueData.GrantType != globals.RefreshTokenGrantType {
		return false
	}
	return true
}

// reserved for future use
// nolint unused
func getUserIP(r *http.Request) string {
	IPAddress := r.Header.Get("X-Real-Ip")
	if IPAddress == "" {
		IPAddress = r.Header.Get("X-Forwarded-For")
	}
	if IPAddress == "" {
		IPAddress = r.RemoteAddr
	}
	return IPAddress
}
