package services

import (
	"time"

	sf "github.com/wissance/stringFormatter"

	"github.com/google/uuid"
	"github.com/wissance/Ferrum/data"
	"github.com/wissance/Ferrum/dto"
	"github.com/wissance/Ferrum/errors"
	"github.com/wissance/Ferrum/logging"
	"github.com/wissance/Ferrum/managers"
)

// TokenBasedSecurityService structure that implements SecurityService
type TokenBasedSecurityService struct {
	DataProvider *managers.DataContext
	UserSessions map[string][]data.UserSession
	logger       *logging.AppLogger
}

// CreateSecurityService creates instance of TokenBasedSecurityService as SecurityService
/* This function creates SecurityService based on dataProvider as managers.DataContext
 * Parameters:
 *    - dataProvider - any managers.DataContext implementation (config.FILE, config.REDIS)
 *    - logger - logger service
 * Returns instance of TokenBasedSecurityService as SecurityService
 */
func CreateSecurityService(dataProvider *managers.DataContext, logger *logging.AppLogger) SecurityService {
	pwdSecService := &TokenBasedSecurityService{DataProvider: dataProvider, UserSessions: map[string][]data.UserSession{}, logger: logger}
	secService := SecurityService(pwdSecService)
	return secService
}

// Validate functions that check whether provided clientId and clientSecret valid or not
/* First this function get find data.Realm data.Client by clientId, if client is data.Public there is nothing to do, for confidential
 * clients function checks provided clientSecret
 * Parameters:
 *    - tokenIssueData data required for issue new token
 *    - realm - obtained from managers.DataContext realm
 * Returns: nil if Validation passed, otherwise error (data.OperationError) with description
 */
func (service *TokenBasedSecurityService) Validate(tokenIssueData *dto.TokenGenerationData, realm *data.Realm) *data.OperationError {
	for _, c := range realm.Clients {
		if c.Name == tokenIssueData.ClientId {
			if c.Type == data.Public {
				service.logger.Trace("Public client was successfully validated")
				return nil
			}

			// here we make deal with confidential client
			if c.Auth.Type == data.ClientIdAndSecrets && c.Auth.Value == tokenIssueData.ClientSecret {
				service.logger.Trace("Private client was successfully validated")
				return nil
			}

		}
	}
	return &data.OperationError{Msg: errors.InvalidClientMsg, Description: errors.InvalidClientCredentialDesc}
}

// CheckCredentials function that checks provided credentials (username and password)
/* This function extracts data.User from DataProvider and also this function checks password from user credentials
 * Parameters:
 *    - tokenIssueData - issues token
 *    - realm - name of a data.Realm
 * Returns: nil if credentials are valid, otherwise error (data.OperationError) with description
 */
func (service *TokenBasedSecurityService) CheckCredentials(tokenIssueData *dto.TokenGenerationData, realmName string) *data.OperationError {
	user, _ := (*service.DataProvider).GetUser(realmName, tokenIssueData.Username)
	if user == nil {
		service.logger.Trace("Credential check: username mismatch")
		return &data.OperationError{Msg: errors.InvalidUserCredentialsMsg, Description: errors.InvalidUserCredentialsDesc}
	}

	realm, err := (*service.DataProvider).GetRealm(realmName)
	if err != nil {
		service.logger.Trace("Credential check: failed to get realm")
		return &data.OperationError{Msg: "failed to get realm", Description: err.Error()}
	}

	if user.IsFederatedUser() {
		msg := sf.Format("User \"{0}\" configured as federated, currently it is not fully supported, wait for future releases",
			user.GetUsername())
		service.logger.Warn(msg)
		return &data.OperationError{Msg: "federated user not supported", Description: msg}
	} else {
		oldPasswordHash := user.GetPasswordHash()
		if !realm.Encoder.IsPasswordsMatch(tokenIssueData.Password, oldPasswordHash) {
			service.logger.Trace("Credential check: password mismatch")
			return &data.OperationError{Msg: errors.InvalidUserCredentialsMsg, Description: errors.InvalidUserCredentialsDesc}
		}
		return nil
	}
}

// GetCurrentUserByName return public user info by username
/* This function simply return user by name, by querying user from DataProvider
 * Parameters:
 *    - realm - realm previously obtained from DataProvider
 *    - userName - name of user
 * Returns user from DataProvider or nil (user not found)
 */
func (service *TokenBasedSecurityService) GetCurrentUserByName(realmName string, userName string) data.User {
	user, _ := (*service.DataProvider).GetUser(realmName, userName)
	return user
}

// GetCurrentUserById return public user info by username
/* This function simply return user by id, by querying user from DataProvider
 * Parameters:
 *    - realm - realm previously obtained from DataProvider
 *    - userId - user identifier
 * Returns user from DataProvider or nil (user not found)
 */
func (service *TokenBasedSecurityService) GetCurrentUserById(realmName string, userId uuid.UUID) data.User {
	user, _ := (*service.DataProvider).GetUserById(realmName, userId)
	return user
}

// StartOrUpdateSession this function starts new session or updates existing one
/* This function starts new session when user successfully gets access token, duration && refresh takes from data.Realm data.Client
 * Sessions storing in internal memory, probably it will be changed and store as temporary key
 * Parameters:
 *    - realm - realm name
 *    - userId - user identifier
 *    - duration - access token == session duration
 *    - refresh - refresh token duration
 * Returns: identifier of session
 */
func (service *TokenBasedSecurityService) StartOrUpdateSession(realm string, userId uuid.UUID, duration int, refresh int) uuid.UUID {
	realmSessions, ok := service.UserSessions[realm]
	sessionId := uuid.New()
	// if there are no realm sessions ...
	if !ok {
		started := time.Now()
		userSession := data.UserSession{
			Id: sessionId, UserId: userId, Started: started,
			Expired:        started.Add(time.Second * time.Duration(duration)),
			RefreshExpired: started.Add(time.Second * time.Duration(refresh)),
		}
		service.UserSessions[realm] = append(realmSessions, userSession)
		return sessionId
	}
	// realm session exists, we should find and update Expired values OR add new
	for i, s := range realmSessions {
		if s.UserId == userId {
			realmSessions[i].Expired = time.Now().Add(time.Second * time.Duration(duration))
			realmSessions[i].RefreshExpired = time.Now().Add(time.Second * time.Duration(refresh))
			service.UserSessions[realm] = realmSessions
			return s.Id
		}
	}
	// such session does not exist, adding
	userSession := data.UserSession{
		Id: sessionId, UserId: userId, Started: time.Now(),
		Expired: time.Now().Add(time.Second * time.Duration(duration)),
	}
	service.UserSessions[realm] = append(realmSessions, userSession)
	return userSession.Id
}

// AssignTokens saves obtained tokens in existing UserSession
/* This function saves tokens in existing session searching it by userId (session must exist)
 * Parameters:
 *    - realm - name of realm
 *    - userId - user identifier
 *    - accessToken - obtained access token
 *    - refreshToken - obtained refresh token
 * Returns nothing
 */
func (service *TokenBasedSecurityService) AssignTokens(realm string, userId uuid.UUID, accessToken *string, refreshToken *string) {
	realmSessions, ok := service.UserSessions[realm]
	if ok {
		// index := -1
		for i, s := range realmSessions {
			if s.UserId == userId {
				realmSessions[i].JwtAccessToken = *accessToken
				realmSessions[i].JwtRefreshToken = *refreshToken
				service.UserSessions[realm] = realmSessions
				break
			}
		}
	}
}

// GetSession returns user session related to user
/* Function iterates over sessions and searches appropriate session by comparing userId with s.UserId
 * Parameters:
 *    - realm - name of a realm
 *    - userId - user identifier
 * Returns data.UserSession if found or nil
 */
func (service *TokenBasedSecurityService) GetSession(realm string, userId uuid.UUID) *data.UserSession {
	realmSessions, ok := service.UserSessions[realm]
	if !ok {
		return nil
	}
	for _, s := range realmSessions {
		if s.UserId == userId {
			return &s
		}
	}
	return nil
}

// GetSessionByAccessToken returns user session related to user by access token
/* Function iterates over sessions and searches appropriate session by comparing token with s.JwtAccessToken
 * Parameters:
 *    - realm - name of a realm
 *    - token - access token
 * Returns data.UserSession if found or nil
 */
func (service *TokenBasedSecurityService) GetSessionByAccessToken(realm string, token *string) *data.UserSession {
	realmSessions, ok := service.UserSessions[realm]
	if !ok {
		return nil
	}
	for _, s := range realmSessions {
		if s.JwtAccessToken == *token {
			return &s
		}
	}
	return nil
}

// GetSessionByRefreshToken returns user session related to user by refresh token
/* Function iterates over sessions and searches appropriate session by comparing token with s.JwtRefreshToken
 * Parameters:
 *    - realm - name of a realm
 *    - token - refresh token
 * Returns data.UserSession if found or nil
 */
func (service *TokenBasedSecurityService) GetSessionByRefreshToken(realm string, token *string) *data.UserSession {
	realmSessions, ok := service.UserSessions[realm]
	if !ok {
		return nil
	}
	for _, s := range realmSessions {
		if s.JwtRefreshToken == *token {
			return &s
		}
	}
	return nil
}

// CheckSessionAndRefreshExpired this function checks both token are expired or not
/* This function compares current time with expiration time (usually refresh token expires earlier than access)
 * Parameters:
 *    - realm - name of a realm
 *    - userId - user identifier
 * Returns tuple of (bool, bool) with values for access token (first) and refresh token (second) expired. If token expired value is true.
 */
func (service *TokenBasedSecurityService) CheckSessionAndRefreshExpired(realm string, userId uuid.UUID) (bool, bool) {
	s := service.GetSession(realm, userId)
	if s == nil {
		return true, true
	}
	current := time.Now().In(time.UTC)
	return s.Expired.In(time.UTC).Before(current), s.RefreshExpired.In(time.UTC).Before(current)
}
