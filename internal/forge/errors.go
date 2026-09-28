package forge

import (
	"fmt"
	"strings"
)

// UnsupportedError reports a forge name nothing implements.
type UnsupportedError struct {
	Forge     string
	Supported []string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("unsupported forge %q, want one of %s",
		e.Forge, strings.Join(e.Supported, ", "))
}

// AuthError reports a credential the forge presented and had refused, as
// distinct from one that was never configured.
type AuthError struct {
	Forge string
	Slug  string
	// Status is the HTTP status that reported it, 0 when there was none.
	Status int
}

func (e *AuthError) Error() string {
	subject := e.Forge
	if e.Slug != "" {
		subject += " workspace " + e.Slug
	}
	if e.Status == 0 {
		return "the credential for " + subject + " was refused"
	}
	return fmt.Sprintf("the credential for %s was refused (HTTP %d)", subject, e.Status)
}

// NotFoundError reports an account the forge does not have, which is almost
// always a typo in the slug and never worth retrying.
type NotFoundError struct {
	Forge string
	Slug  string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s has no account %q", e.Forge, e.Slug)
}

// UnavailableError reports a forge that could not be reached or answered with
// something a retry might get past.
type UnavailableError struct {
	Forge  string
	Status int
	Err    error
}

func (e *UnavailableError) Error() string {
	switch {
	case e.Status != 0:
		return fmt.Sprintf("%s is unavailable (HTTP %d)", e.Forge, e.Status)
	case e.Err != nil:
		return fmt.Sprintf("%s is unreachable: %v", e.Forge, e.Err)
	default:
		return e.Forge + " is unavailable"
	}
}

func (e *UnavailableError) Unwrap() error { return e.Err }

// ResponseError reports an answer that arrived and could not be read, which is
// a defect in this tool or a forge that changed its API - never something the
// user can fix by retrying.
type ResponseError struct {
	Forge string
	URL   string
	Err   error
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("%s answered %s with something unreadable: %v", e.Forge, e.URL, e.Err)
}

func (e *ResponseError) Unwrap() error { return e.Err }

// Classify turns an HTTP status into this package's error vocabulary, in one
// place, so every forge maps a status the same way and none of them invents a
// third meaning for 403.
func Classify(name, slug, url string, status int) error {
	switch {
	case status == 401 || status == 403:
		return &AuthError{Forge: name, Slug: slug, Status: status}
	case status == 404:
		return &NotFoundError{Forge: name, Slug: slug}
	case status >= 400:
		return &UnavailableError{Forge: name, Status: status}
	default:
		return nil
	}
}
