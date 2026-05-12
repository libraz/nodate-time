package users

import "time"

type UserResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Icon      string    `json:"icon"`
	Color     string    `json:"color"`
	AvatarURL string    `json:"avatarUrl,omitempty"`
	IsAdmin   bool      `json:"isAdmin"`
	CreatedAt time.Time `json:"createdAt"`
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

type RequestResetInput struct {
	Body struct {
		Email string `json:"email" format:"email"`
	}
}

type RequestResetOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

type ConfirmResetInput struct {
	Body struct {
		Token       string `json:"token" minLength:"16" maxLength:"128"`
		NewPassword string `json:"newPassword" minLength:"8" maxLength:"128"`
	}
}

type ConfirmResetOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

type OAuthStartInput struct {
	Provider string `path:"provider" enum:"google,line"`
	Redirect string `query:"redirect"`
}

type OAuthStartOutput struct {
	Status int
	URL    string `header:"Location"`
	Body   struct {
		AuthorizeURL string `json:"authorizeUrl"`
		State        string `json:"state"`
	}
}

type OAuthCallbackInput struct {
	Provider string `path:"provider" enum:"google,line"`
	Code     string `query:"code"`
	State    string `query:"state"`
}

type OAuthCallbackOutput struct {
	Status int
	URL    string `header:"Location"`
}

// --- Avatar ---

type PresignAvatarInput struct {
	Body struct {
		ContentType string `json:"contentType" doc:"MIME type, e.g. image/jpeg"`
		ByteSize    int64  `json:"byteSize" minimum:"1" doc:"File size in bytes"`
	}
}

type PresignAvatarOutput struct {
	Body struct {
		AvatarID   string `json:"avatarId" doc:"Opaque ID to send back to ConfirmAvatar"`
		UploadURL  string `json:"uploadUrl" doc:"Presigned PUT URL, valid for 15 minutes"`
		StorageKey string `json:"storageKey" doc:"Internal key, mostly for debugging"`
	}
}

type ConfirmAvatarInput struct {
	Body struct {
		AvatarID string `json:"avatarId" minLength:"1"`
	}
}

type ConfirmAvatarOutput struct {
	Body UserResponse
}

type DeleteAvatarInput struct{}

type DeleteAvatarOutput struct {
	Body UserResponse
}
