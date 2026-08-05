package users

import "time"

type UserResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatarUrl,omitempty"`
	Locale    string `json:"locale"`
	Timezone  string `json:"timezone"`
	// IsAdmin reflects a live grant in instance_admins, not a column on the
	// user: revoking it leaves the record that it was ever held.
	IsAdmin bool `json:"isAdmin"`
	// EmailVerified reports whether this address has been confirmed by
	// following a link sent to it. Provider sign-in refuses to link to an
	// account that has not, so the client surfaces it.
	EmailVerified bool      `json:"emailVerified"`
	CreatedAt     time.Time `json:"createdAt"`
}

// RefreshInput trades a refresh token for a new pair.
type RefreshInput struct {
	Body struct {
		RefreshToken string `json:"refreshToken" minLength:"1"`
	}
}
type RefreshOutput struct {
	Body struct {
		Token        string       `json:"token"`
		RefreshToken string       `json:"refreshToken"`
		User         UserResponse `json:"user"`
	}
}

type LogoutInput struct{}
type LogoutOutput struct{}

type RegisterInput struct {
	Body struct {
		Name     string `json:"name" minLength:"1" maxLength:"100" doc:"User name"`
		Email    string `json:"email" format:"email" doc:"Email address"`
		Password string `json:"password" minLength:"8" maxLength:"128" doc:"Password"`
	}
}

type RegisterOutput struct {
	Body struct {
		Token        string       `json:"token"`
		RefreshToken string       `json:"refreshToken"`
		User         UserResponse `json:"user"`
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
		Token        string       `json:"token"`
		RefreshToken string       `json:"refreshToken"`
		User         UserResponse `json:"user"`
	}
}

type DevLoginInput struct {
	Body struct {
		Email string `json:"email" format:"email" doc:"Email of a seeded dev account (@example.com only)"`
	}
}

type DevLoginOutput struct {
	Body struct {
		Token        string       `json:"token"`
		RefreshToken string       `json:"refreshToken"`
		User         UserResponse `json:"user"`
	}
}

type GetMeInput struct{}
type GetMeOutput struct {
	Body UserResponse
}

type UpdateMeInput struct {
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
		// Locale and Timezone are optional; an empty value keeps the current
		// setting rather than clearing a column that cannot be null.
		Locale   string `json:"locale,omitempty" maxLength:"35" required:"false"`
		Timezone string `json:"timezone,omitempty" maxLength:"64" required:"false" doc:"IANA timezone"`
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

type ChangePasswordOutput struct {
	Body struct {
		// Changing the credential revokes every session opened with the old
		// one. This pair belongs to a fresh session for the device that made
		// the change, so it stays signed in while the others do not.
		Token        string `json:"token"`
		RefreshToken string `json:"refreshToken"`
	}
}

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

type OAuthProvidersInput struct{}

type OAuthProvidersOutput struct {
	Body struct {
		// Providers lists the OAuth providers that are configured and enabled,
		// so the login screen can render only the buttons that actually work.
		Providers []string `json:"providers"`
		// PasswordEnabled reports whether email+password sign-in is available.
		// When false the login screen shows OAuth/OIDC sign-in only.
		PasswordEnabled bool `json:"passwordEnabled"`
	}
}

type OAuthStartInput struct {
	Provider string `path:"provider" enum:"google,line"`
	Redirect string `query:"redirect"`
}

type OAuthStartOutput struct {
	Status int
	URL    string `header:"Location"`
	// SetCookie binds the OAuth flow to the initiating browser: the callback only
	// proceeds if it presents this same state cookie, defeating login CSRF.
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		AuthorizeURL string `json:"authorizeUrl"`
		State        string `json:"state"`
	}
}

type OAuthCallbackInput struct {
	Provider string `path:"provider" enum:"google,line"`
	Code     string `query:"code" required:"false"`
	State    string `query:"state" required:"false"`
	// Error and ErrorDescription carry the provider's own failure report (e.g.
	// "access_denied" when the user cancels consent). Neither Code nor State is
	// guaranteed to be usable when Error is set.
	Error       string `query:"error" required:"false"`
	ErrorDesc   string `query:"error_description" required:"false"`
	StateCookie string `cookie:"oauth_state"`
}

type OAuthCallbackOutput struct {
	Status int
	URL    string `header:"Location"`
	// SetCookie clears the one-time state cookie once the flow completes.
	SetCookie string `header:"Set-Cookie"`
}

// --- Avatar ---

type PresignAvatarInput struct {
	Body struct {
		ContentType string `json:"contentType" doc:"MIME type, e.g. image/jpeg"`
		ByteSize    int64  `json:"byteSize" minimum:"1" doc:"File size in bytes"`
		// SHA256 is the digest of the bytes about to be uploaded. The blob is
		// content-addressed, so the same picture is stored once no matter how
		// many people set it; the digest is also what the storage key is
		// built from.
		SHA256 string `json:"sha256" minLength:"64" maxLength:"64" pattern:"^[0-9a-fA-F]{64}$" doc:"lowercase hex SHA-256 of the file contents"`
	}
}

type PresignAvatarOutput struct {
	Body struct {
		AvatarID  string `json:"avatarId" doc:"Opaque ID to send back to ConfirmAvatar"`
		UploadURL string `json:"uploadUrl" doc:"Presigned PUT URL, valid for 15 minutes"`
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

// ResendVerificationInput carries no fields: the address is whichever one the
// signed-in account currently holds, never one the caller names.
type ResendVerificationInput struct{}

type ResendVerificationOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

type ConfirmVerificationInput struct {
	Body struct {
		Token string `json:"token" minLength:"16" maxLength:"128"`
	}
}

type ConfirmVerificationOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}
