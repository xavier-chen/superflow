//go:build !desktop

package desktop

import (
	"context"
	"errors"
)

func Available() bool          { return false }
func ReportStartupError(error) {}
func Run(context.Context, string, ...Options) error {
	return errors.New("此构建仅包含核心服务；请打开 SuperFlow 桌面应用，或使用 go run -tags desktop ./cmd/superflow ui")
}
