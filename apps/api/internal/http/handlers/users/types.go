package users

import "time"

type UserResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Icon        string    `json:"icon"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"createdAt"`
}

type RegisterInput struct {
	Body struct {
		Name     string `json:"name" minLength:"1" maxLength:"100" doc:"User name"`
		Email    string `json:"email" format:"email" doc:"Email address"`
		Password string `json:"password" minLength:"8" maxLength:"128" doc:"Password"`
	}
}

type RegisterOutput struct {
	Body struct {
		Token string       `json:"token"`
		User  UserResponse `json:"user"`
	}
}

type LoginInput struct {
	Body struct {
		Email    string `json:"email" format:"email"`
		Password string `json:"password"`
	}
}

type LoginOutput struct {
	Body struct {
		Token string       `json:"token"`
		User  UserResponse `json:"user"`
	}
}

type GetMeInput struct{}
type GetMeOutput struct {
	Body UserResponse
}

type UpdateMeInput struct {
	Body struct {
		Name  string `json:"name" minLength:"1" maxLength:"100"`
		Icon  string `json:"icon" maxLength:"10"`
		Color string `json:"color" maxLength:"7"`
	}
}

type UpdateMeOutput struct {
	Body UserResponse
}

type ChangePasswordInput struct {
	Body struct {
		CurrentPassword string `json:"currentPassword" minLength:"1" doc:"Current password"`
		NewPassword     string `json:"newPassword" minLength:"8" maxLength:"128" doc:"New password"`
	}
}

type ChangePasswordOutput struct{}
