package services

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/wissance/Ferrum/data"
	"github.com/wissance/Ferrum/logging"
	"github.com/wissance/stringFormatter"
)

// JwtGenerator is useful struct that has methods to generate JWT tokens using golang-jwt utility
type JwtGenerator struct {
	// TODO(UMV): we should add possibility to regenerate SignKey (probably via CLI)
	SignKey []byte
	Logger  *logging.AppLogger
}

// GenerateJwtAccessToken generates encoded string of access token in JWT format
/* This function combines a lot of arguments into one big JSON and encode it using SignKey (should be loaded by application)
 * Parameters:
 *    - realmBaseUrl - common path of all routes, usually ~/auth/realms/{realm}/ (see api/rest/getRealmBaseUrl)
 *    - tokenType - string with type of token, rest.Bearer
 *    - scope - verification scope, currently used only globals.ProfileEmailScope
 *    - sessionData - full session data of authorized user
 *    - userData - full public user data
 * Returns: JWT-encoded string with access token
 */
func (generator *JwtGenerator) GenerateJwtAccessToken(realmBaseUrl string, tokenType string, scope string, sessionData *data.UserSession,
	userData data.User,
) string {
	accessToken := generator.prepareAccessToken(realmBaseUrl, tokenType, scope, sessionData, userData)
	return generator.generateJwtAccessToken(accessToken)
}

// GenerateJwtRefreshToken generates encoded string of refresh token in JWT format
/* This function combines a lot of arguments into one big JSON and encode it using SignKey (should be loaded by application).
 * FULLY SIMILAR To GenerateJwtAccessToken except it has not userData like previous func
 * Parameters:
 *    - realmBaseUrl - common path of all routes, usually ~/auth/realms/{realm}/ (see api/rest/getRealmBaseUrl)
 *    - tokenType - string with type of token, rest.Refresh
 *    - scope - verification scope, currently used only globals.ProfileEmailScope
 *    - sessionData - full session data of authorized user
 * Returns: JWT-encoded string with refresh token
 */
func (generator *JwtGenerator) GenerateJwtRefreshToken(realmBaseUrl string, tokenType string, scope string, sessionData *data.UserSession) string {
	refreshToken := generator.prepareRefreshToken(realmBaseUrl, tokenType, scope, sessionData)
	return generator.generateJwtRefreshToken(refreshToken)
}

// generateJwtAccessToken this is actual access token JWT generation with SignKey as a Token signature (HMAC-SHA-256)
func (generator *JwtGenerator) generateJwtAccessToken(tokenData *data.AccessTokenData) string {
	token := jwt.New(jwt.SigningMethodHS256)
	// signed token contains embedded type because we don't actually know type of User, therefore we do it like jwt do but use RawStr
	signedToken, err := generator.makeSignedToken(token, tokenData, generator.SignKey)
	// token.SignedString([]byte("secureSecretText"))
	if err != nil {
		// todo(UMV): think what to do on Error
		generator.Logger.Error(stringFormatter.Format("An error occurred during signed Jwt Access Token Generation: {0}", err.Error()))
	}

	return signedToken
}

// generateJwtAccessToken this is actual refresh token JWT generation with SignKey as a Token signature (HMAC-SHA-256)
func (generator *JwtGenerator) generateJwtRefreshToken(tokenData *data.TokenRefreshData) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenData)
	signedToken, err := token.SignedString(generator.SignKey)
	if err != nil {
		// todo(UMV): think what to do on Error
		generator.Logger.Error(stringFormatter.Format("An error occurred during signed Jwt Refresh Token Generation: {0}", err.Error()))
	}
	return signedToken
}

// prepareAccessToken builds data.AccessTokenData from a lot of params
func (generator *JwtGenerator) prepareAccessToken(realmBaseUrl string, tokenType string, scope string, sessionData *data.UserSession,
	userData data.User,
) *data.AccessTokenData {
	issuer := realmBaseUrl
	jwtCommon := data.JwtCommonInfo{
		Issuer: issuer, Type: tokenType, Audience: "account", Scope: scope, JwtId: uuid.New(),
		IssuedAt: sessionData.Started.Unix(), ExpiredAt: sessionData.Expired.Unix(), Subject: sessionData.UserId,
		SessionId: sessionData.Id, SessionState: sessionData.Id,
	}
	accessToken := data.CreateAccessToken(&jwtCommon, userData)
	return accessToken
}

// prepareRefreshToken builds data.TokenRefreshData from a lot of params
func (generator *JwtGenerator) prepareRefreshToken(realmBaseUrl string, tokenType string, scope string, sessionData *data.UserSession) *data.TokenRefreshData {
	issuer := realmBaseUrl
	jwtCommon := data.JwtCommonInfo{
		Issuer: issuer, Type: tokenType, Audience: issuer, Scope: scope, JwtId: uuid.New(),
		IssuedAt: sessionData.Started.Unix(), ExpiredAt: sessionData.Expired.Unix(), Subject: sessionData.UserId,
		SessionId: sessionData.Id, SessionState: sessionData.Id,
	}
	accessToken := data.CreateRefreshToken(&jwtCommon)
	return accessToken
}

// makeSignedToken this function adds signature to token
func (generator *JwtGenerator) makeSignedToken(token *jwt.Token, tokenData *data.AccessTokenData, signKey interface{}) (string, error) {
	var err error
	var sig string
	var jsonValue []byte

	if jsonValue, err = json.Marshal(token.Header); err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString(jsonValue)

	claim := base64.RawURLEncoding.EncodeToString([]byte(tokenData.ResultJsonStr))

	unsignedToken := strings.Join([]string{header, claim}, ".")
	if sig, err = token.Method.Sign(unsignedToken, signKey); err != nil {
		return "", err
	}
	return strings.Join([]string{unsignedToken, sig}, "."), nil
}
