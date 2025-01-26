package federation

import (
	"errors"

	"github.com/go-ldap/ldap/v3"
	"github.com/wissance/Ferrum/data"
	appErrs "github.com/wissance/Ferrum/errors"
	"github.com/wissance/Ferrum/logging"
	sf "github.com/wissance/stringFormatter"
)

const userNameLdapFilterTemplate = "(SAMAccountName={0})"

// LdapUserFederation is a service that is responsible for User Federation using Ldap protocol
// todo(UMV): this is a preliminary implementation (not tested yet)
type LdapUserFederation struct {
	logger *logging.AppLogger
	config *data.UserFederationServiceConfig
}

func CreateLdapUserFederationService(config *data.UserFederationServiceConfig, logger *logging.AppLogger) (*LdapUserFederation, error) {
	return &LdapUserFederation{config: config, logger: logger}, nil
}

// GetUser builds user from data from federation service
/*
 * Useful resources:
 * 1. https://dev.to/openlab/ldap-authentication-in-golang-with-bind-and-search-47h5
 */
func (s *LdapUserFederation) GetUser(userName string, mask string) (data.User, error) {
	// todo(UMV): add TLS config
	conn, err := ldap.DialURL(s.config.Url)
	defer func() {
		_ = conn.Close()
	}()
	if err != nil {
		s.logger.Error(sf.Format("An error occurred during LDAP URL dial: {0}", err.Error()))
		return nil, err
	}
	if s.config.IsAnonymousAccess() {
		err = conn.UnauthenticatedBind("")
		if err != nil {
			s.logger.Error(sf.Format("An error occurred during LDAP Unauthenticated bind: {0}", err.Error()))
			return nil, err
		}
	} else {
		err = conn.Bind(s.config.SysUser, s.config.SysPassword)
		if err != nil {
			s.logger.Error(sf.Format("An error occurred during LDAP Bind: {0}", err.Error()))
			return nil, err
		}
	}
	// Search for a user ...
	userFilter := sf.Format(userNameLdapFilterTemplate, userName)
	searchReq := ldap.NewSearchRequest(s.config.EntryPoint, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		0, 0, false, userFilter, []string{}, nil)

	result, err := conn.Search(searchReq)
	if err != nil {
		s.logger.Error(sf.Format("An error occurred during LDAP Request Search: {0}", err.Error()))
		return nil, err
	}

	if result != nil {
		if len(result.Entries) == 0 {
			return nil, appErrs.NewFederatedUserNotFound(string(s.config.Type), s.config.Name, s.config.Url, userName)
		}

		if len(result.Entries) > 1 {
			return nil, appErrs.NewMultipleUserResultError(s.config.Name, userName)
		}
	}

	// todo(UMV): convert []Attributes to Json and pass
	// result.Entries[0].Attributes[0].Name

	return nil, nil
}

func (s *LdapUserFederation) GetUsers(mask string) []data.User {
	return []data.User{}
}

func (s *LdapUserFederation) Authenticate(userName string, password string) (bool, error) {
	return false, errors.New("not implemented yet")
}

func (s *LdapUserFederation) Init() {
}
