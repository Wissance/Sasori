package data

import (
	"github.com/google/uuid"
	"github.com/wissance/Ferrum/utils/jsontools"
)

// RawUserInfo is a type that is using for place all public user data (in Keycloak - "info":{...} struct) into JWT encoded token
type RawUserInfo interface{}

// JwtCommonInfo - struct with all field for representing token in JWT format
type JwtCommonInfo struct {
	IssuedAt     int64     `json:"iat"`
	ExpiredAt    int64     `json:"exp"`
	JwtId        uuid.UUID `json:"jti"`
	Type         string    `json:"typ"`
	Issuer       string    `json:"iss"`
	Audience     string    `json:"aud"`
	Subject      uuid.UUID `json:"sub"`
	SessionState uuid.UUID `json:"session_state"`
	SessionId    uuid.UUID `json:"sid"`
	Scope        string    `json:"scope"`
}

// TokenRefreshData is a JWT token with embedded just a common data (JwtCommonInfo)
type TokenRefreshData struct {
	JwtCommonInfo
}

// AccessTokenData is a struct that stores data for build JWT access token (jwtCommonInfo, rawUserInfo) and result (ResultData, ResultJsonStr)
// this token = jwtCommonInfo + rawUserInfo
type AccessTokenData struct {
	jwtCommonInfo JwtCommonInfo
	rawUserInfo   RawUserInfo
	ResultData    map[string]interface{}
	ResultJsonStr string
}

// CreateAccessToken creates new AccessToken from common token data and public user info
func CreateAccessToken(commonData *JwtCommonInfo, userData User) *AccessTokenData {
	token := AccessTokenData{jwtCommonInfo: *commonData, rawUserInfo: userData.GetUserInfo()}
	token.Init()
	return &token
}

// Valid is using for checking token fields values contains proper values, temporarily doesn't do anything
func (token *AccessTokenData) Valid() error {
	// just pass formally, we don't have anything to validate, maybe in future
	return nil
}

// CreateRefreshToken creates Refresh token
func CreateRefreshToken(commonData *JwtCommonInfo) *TokenRefreshData {
	return &TokenRefreshData{JwtCommonInfo: *commonData}
}

// Valid is using for checking token fields values contains proper values, temporarily doesn't do anything
func (token *TokenRefreshData) Valid() error {
	// just pass formally, we don't have anything to validate, maybe in future
	return nil
}

// Init - combines 2 fields into map (ResultJsonStr) and simultaneously in a marshalled string ResultJsonStr
func (token *AccessTokenData) Init() {
	data, str := jsontools.MergeNonIntersect[JwtCommonInfo, RawUserInfo](&token.jwtCommonInfo, &token.rawUserInfo)
	token.ResultData = data.(map[string]interface{})
	token.ResultJsonStr = str
}
