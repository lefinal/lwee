// Package input allows requesting input from the user.
package input

import (
	"context"
	"fmt"
	"github.com/lefinal/meh"
	"github.com/manifoldco/promptui"
	"strings"
)

// Input provides methods for prompting the user for inputs.
type Input interface {
	// RequestConfirm prompts the user with the given one for confirmation. If no
	// input was provided, the given default value will be returned.
	RequestConfirm(ctx context.Context, prompt string, defaultValue bool) (bool, error)

	// Request prompts the user with the given one for an input.
	Request(ctx context.Context, prompt string, validate func(s string) error) (string, error)

	RequestSelection(ctx context.Context, prompt string, options []string) (int, string, error)
}

// Stdin implements Input using stdin.
type Stdin struct {
}

// RequestConfirm prompts the user with the given one for confirmation. If no
// input was provided, the given default value will be returned.
func (input *Stdin) RequestConfirm(ctx context.Context, prompt string, defaultValue bool) (bool, error) {
	defaultValueStr := "n"
	if defaultValue {
		defaultValueStr = "y"
	}

	for {
		myPrompt := promptui.Prompt{
			Label:     prompt,
			Default:   defaultValueStr,
			IsConfirm: true,
		}
		resultStr, err := myPrompt.Run()
		if err == nil || err.Error() == "" {
			// OK.
			switch strings.ToLower(resultStr) {
			case "y":
				return true, nil
			case "n":
				return false, nil
			case "":
				return defaultValue, nil
			}
		}
		// Error or invalid value.
		if shouldAbortPrompt(ctx, err) {
			return false, meh.NewBadInputErr("canceled", nil)
		}
		fmt.Println(createErrorMessage("invalid value entered", err, resultStr))
	}

}

func shouldAbortPrompt(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	if err != nil && err.Error() == "^C" {
		return true
	}
	return false
}

// createErrorMessage creates an error message by combining the given message,
// error, and value.
func createErrorMessage(message string, err error, value string) string {
	errDescription := message
	if err.Error() != "" {
		errDescription += fmt.Sprintf(" (%s)", err.Error())
	}
	if value != "" {
		errDescription += fmt.Sprintf(": %s", value)
	}
	return errDescription
}

// Request prompts the user with the given one for an input.
//
// The provided prompt should be in the format of "Enter xyz". RequestInput will
// append colons.
func (input *Stdin) Request(ctx context.Context, prompt string, validate func(s string) error) (string, error) {
	for {
		myPrompt := promptui.Prompt{
			Label:    prompt,
			Validate: validate,
		}
		result, err := myPrompt.Run()
		if err == nil {
			// OK.
			return result, nil
		}
		// Error or invalid value.
		if shouldAbortPrompt(ctx, err) {
			return "", meh.NewBadInputErr("canceled", nil)
		}
		fmt.Println(createErrorMessage("invalid value entered", err, result))
	}
}

// RequestSelection prompts the user with the given options and returns the
// selected index and value. If an error occurs or an invalid value is entered,
// it will display an error message and prompt again.
func (input *Stdin) RequestSelection(ctx context.Context, prompt string, options []string) (int, string, error) {
	for {
		myPrompt := promptui.Select{
			Label: prompt,
			Items: options,
		}
		resultIndex, result, err := myPrompt.Run()
		if err == nil {
			// OK.
			return resultIndex, result, nil
		}
		// Error or invalid value.
		if shouldAbortPrompt(ctx, err) {
			return 0, "", meh.NewBadInputErr("canceled", nil)
		}
		fmt.Println(createErrorMessage("invalid value entered", err, result))
	}
}
