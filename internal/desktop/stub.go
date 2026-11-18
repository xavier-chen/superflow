//go:build !desktop

package desktop

import (
	"context"
	"errors"

	"superflow.local/superflow/internal/i18n"
)

func Available() bool          { return false }
func ReportStartupError(error) {}
func Run(context.Context, string, ...Options) error {
	return errors.New(i18n.T("This build contains the core service. Open the desktop app or use go run -tags desktop ./cmd/superflow ui"))
}
