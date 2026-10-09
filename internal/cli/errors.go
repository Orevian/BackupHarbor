package cli

import (
	"errors"

	"backupharbor/internal/verify"
)

func asErr[T any](err error, target *T) bool { return errors.As(err, target) }

func isDep(err error) bool {
	var d verify.ErrDependency
	return errors.As(err, &d)
}

func isNoTargets(err error) bool { return errors.Is(err, verify.ErrNoTargets) }

func isUnknownTarget(err error, x *ctx) bool {
	var u verify.UnknownTargetError
	if errors.As(err, &u) {
		x.fail(ExitError, x.t("cli.unknown_target", u.Name))
		return true
	}
	return false
}
