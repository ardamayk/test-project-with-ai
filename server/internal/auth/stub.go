package auth

import "net/http"

const DefaultUserID = "00000000-0000-0000-0000-000000000001"

type User struct {
	ID          string
	Username    string
	DisplayName string
}

type CurrentUserProvider interface {
	CurrentUser(r *http.Request) (User, error)
}

type StubCurrentUserProvider struct{}

func (StubCurrentUserProvider) CurrentUser(_ *http.Request) (User, error) {
	return User{
		ID:          DefaultUserID,
		Username:    "admin",
		DisplayName: "Admin",
	}, nil
}

var DefaultCurrentUserProvider CurrentUserProvider = StubCurrentUserProvider{}

func CurrentUser(r *http.Request) (User, error) {
	return DefaultCurrentUserProvider.CurrentUser(r)
}

func CurrentUserID(r *http.Request) (string, error) {
	user, err := CurrentUser(r)
	if err != nil {
		return "", err
	}
	return user.ID, nil
}
