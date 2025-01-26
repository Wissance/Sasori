package data

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wissance/Ferrum/utils/encoding"
	sf "github.com/wissance/stringFormatter"
)

func TestInitUserWithJsonAndCheck(t *testing.T) {
	testCases := []struct {
		name              string
		userName          string
		preferredUsername string
		isFederated       bool
		userTemplate      string
		federationId      string
	}{
		{
			name: "simple_user", userName: "admin", preferredUsername: "Administrator", isFederated: false,
			userTemplate: `{"info":{"name":"{0}", "preferred_username": "{1}"}}`,
		},
		{
			name: "federated_user", userName: `m.ushakov`, preferredUsername: "m.ushakov", isFederated: true, federationId: "Wissance_test_domain",
			userTemplate: `{"info":{"name":"{0}", "preferred_username": "{1}"}, "federation":{"name":"Wissance_test_domain"}}`,
		},
		{
			name: "federated_user", userName: `root`, preferredUsername: "root", isFederated: false,
			userTemplate: `{"info":{"name":"{0}", "preferred_username": "{1}"}, "federation":{"cfg":{}}}`,
		},
	}

	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			jsonStr := sf.Format(tCase.userTemplate, tCase.userName, tCase.preferredUsername)
			var rawUserData interface{}
			err := json.Unmarshal([]byte(jsonStr), &rawUserData)
			assert.NoError(t, err)
			encoder := encoding.NewPasswordJsonEncoder("salt")
			user := CreateUser(rawUserData, encoder)
			assert.Equal(t, tCase.preferredUsername, user.GetUsername())
			assert.Equal(t, tCase.isFederated, user.IsFederatedUser())
			if user.IsFederatedUser() {
				assert.Equal(t, tCase.federationId, user.GetFederationId())
			}
		})
	}
}
