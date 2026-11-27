//go:build linux && cgo && !nopam

package auth

import (
	"errors"

	"github.com/msteinert/pam/v2"
)

func authenticatePAM(username, password string) error {
	transaction, err := pam.StartFunc("simplefsmanager", username, func(style pam.Style, _ string) (string, error) {
		switch style {
		case pam.PromptEchoOff:
			return password, nil
		case pam.PromptEchoOn:
			return username, nil
		default:
			return "", errors.New("unsupported PAM conversation")
		}
	})
	if err != nil {
		return err
	}
	defer transaction.End()
	if err := transaction.Authenticate(pam.DisallowNullAuthtok); err != nil {
		return err
	}
	return transaction.AcctMgmt(pam.DisallowNullAuthtok)
}
