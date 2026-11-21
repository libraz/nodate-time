package router

import (
	"errors"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
)

// huma builds every error it raises itself -- validation failures, resolver
// refusals, unreadable bodies -- through huma.NewError, and derives the default
// error schema of the OpenAPI document from the type it returns. Replacing it
// makes those errors carry a stable code like the ones handlers return.
func init() {
	huma.NewError = newError
}

func newError(status int, msg string, errs ...error) huma.StatusError {
	for _, err := range errs {
		switch {
		case errors.Is(err, precondition.ErrRequired):
			return errorBody(apierrors.PreconditionRequired)
		case errors.Is(err, precondition.ErrMalformed):
			return errorBody(apierrors.BadIfMatch)
		}
	}

	switch {
	case status == http.StatusPreconditionRequired:
		return errorBody(apierrors.PreconditionRequired)
	case status == http.StatusUnprocessableEntity:
		out := errorBody(apierrors.Validation)
		out.Errors = validationDetails(errs)
		return out
	case status >= http.StatusInternalServerError:
		// The message of a server failure can carry internals; the client gets
		// the generic one.
		out := errorBody(apierrors.InternalUnexpected)
		if status != 0 {
			out.Status = status
		}
		return out
	}

	out := errorBody(apierrors.BadRequest)
	out.Status = status
	if status == http.StatusNotFound {
		out.Code, out.Message = apierrors.NotFound.Code, apierrors.NotFound.Message
	} else if msg != "" {
		out.Message = msg
	}
	return out
}

func errorBody(s *apierrors.Spec) *apierrors.HumaError {
	return &apierrors.HumaError{Status: s.Status, Code: s.Code, Message: s.Message}
}

func validationDetails(errs []error) []*huma.ErrorDetail {
	var out []*huma.ErrorDetail
	for _, err := range errs {
		if err == nil {
			continue
		}
		if d, ok := err.(huma.ErrorDetailer); ok {
			out = append(out, d.ErrorDetail())
			continue
		}
		out = append(out, &huma.ErrorDetail{Message: err.Error()})
	}
	return out
}

// errorStatuses lists the response statuses an operation can answer with, for
// huma.Operation.Errors: one entry per distinct status of the given Specs, in
// ascending order.
func errorStatuses(specs ...*apierrors.Spec) []int {
	var out []int
	for _, s := range specs {
		if !slices.Contains(out, s.Status) {
			out = append(out, s.Status)
		}
	}
	slices.Sort(out)
	return out
}
