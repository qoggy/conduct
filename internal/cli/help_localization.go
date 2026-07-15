package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/locale"
	"github.com/qoggy/conduct/internal/message"
)

var selectedLanguage = locale.English

func detectedHelpLanguage() locale.Language {
	return selectedLanguage
}

// localizedHelpText 返回当前环境所选语言的 help 文案。
func localizedHelpText(chinese, english string) string {
	return detectedHelpLanguage().Select(chinese, english)
}

func localizedErrorf(chinese, english string, arguments ...any) error {
	return fmt.Errorf(localizedHelpText(chinese, english), arguments...)
}

func formatCLIError(err error) string {
	if applicationError, ok := apperror.As(err); ok {
		return message.Error(selectedLanguage, applicationError)
	}
	return err.Error()
}
