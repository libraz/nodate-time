package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func uniqueEmail() string {
	return fmt.Sprintf("test-%d@example.com", time.Now().UnixNano())
}

func TestAuthRegisterAndLogin(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := uniqueEmail()

	// Register
	var regResp struct {
		Token string `json:"token"`
		User  struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"user"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/register", "",
		map[string]any{"name": "太郎", "email": email, "password": "password123"},
		&regResp)

	require.NotEmpty(t, regResp.Token)
	require.NotEmpty(t, regResp.User.ID)
	require.Equal(t, "太郎", regResp.User.Name)
	require.Equal(t, email, regResp.User.Email)

	// Login
	var loginResp struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": email, "password": "password123"},
		&loginResp)

	require.NotEmpty(t, loginResp.Token)
	require.Equal(t, regResp.User.ID, loginResp.User.ID)

	// Get /user
	var me struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", loginResp.Token, nil, &me)
	require.Equal(t, "太郎", me.Name)
}

func TestAuthBadCredentials(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": "noone-bad@example.com", "password": "wrong"})
	require.Equal(t, 401, status)
}

func TestAuthNoToken(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	status, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/calendars", "", nil)
	require.Equal(t, 401, status)
}

func TestAuthDuplicateEmail(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := uniqueEmail()
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/register", "",
		map[string]any{"name": "A", "email": email, "password": "password123"}, nil)

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/register", "",
		map[string]any{"name": "B", "email": email, "password": "password123"})
	require.Equal(t, 409, status)
}
