package auth

import "unicode/utf8"

// StorableEmail reports whether an address fits the columns that hold it.
//
// users.email and oauth_allowed_emails.email are latin1 and documented as
// ASCII only, so an address outside ASCII cannot be stored, cannot be
// allow-listed, and cannot be matched against either. Passing one to the
// database produces a charset error the caller reads as a server fault.
//
// The check is ASCII rather than latin1-encodable on purpose. latin1 could
// hold some of these, but the collation folds accented letters onto their
// plain forms, so two addresses that differ would compare equal -- and which
// two accounts collide is an authentication question, not a storage one.
//
// This is a staging post, not an answer. An internationalised address is a
// real address and refusing it tells its owner to get another one; the end
// state is those columns holding what people actually have. Moving them is
// authentication work rather than a schema change -- all three participate in
// unique keys and cross-table matching, so it needs a normalisation decision
// first (case folding, and whether to keep a normalised form beside the
// display form). This function is the single place that changes when they do.
func StorableEmail(email string) bool {
	for i := 0; i < len(email); i++ {
		if email[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
